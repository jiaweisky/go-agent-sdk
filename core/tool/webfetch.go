package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
)

const WebFetchName = "webfetch"

const (
	WebFetchMaxResponseBytes      = 5 * 1024 * 1024
	WebFetchDefaultTimeoutSeconds = 30
	WebFetchMaxTimeoutSeconds     = 120
)

//nolint:lll // prose description
const WebFetchDescription = `Fetch content from an HTTP or HTTPS URL and return it as text, markdown, or HTML. Markdown is the default.

Use a more targeted tool when one is available. This tool is read-only. Large text results may be replaced with a preview while the complete output is retained in managed storage.`

var WebFetchInputSchema = jsonschema.Object(
	jsonschema.Required("url", jsonschema.String(jsonschema.Description("The HTTP or HTTPS URL to fetch content from"))),
	jsonschema.Optional("format", jsonschema.Enum(
		[]any{"text", "markdown", "html"},
		jsonschema.Description("The format to return the content in. Defaults to markdown."),
	)),
	jsonschema.Optional("timeout", jsonschema.Number(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Maximum(WebFetchMaxTimeoutSeconds),
		jsonschema.Description(fmt.Sprintf("Optional timeout in seconds (maximum: %d)", WebFetchMaxTimeoutSeconds)),
	)),
)

var WebFetchOutputSchema = jsonschema.Object(
	jsonschema.Required("url", jsonschema.String()),
	jsonschema.Required("contentType", jsonschema.String()),
	jsonschema.Required("format", jsonschema.Literal("text", "markdown", "html")),
	jsonschema.Required("output", jsonschema.String()),
)

type WebFetchInput struct {
	URL     string   `json:"url"`
	Format  string   `json:"format"`
	Timeout *float64 `json:"timeout,omitempty"`
}

// UnmarshalJSON applies the schema decoding default for format: when the field
// is absent it becomes "markdown", mirroring Schema.withDecodingDefault. The
// remaining Input constraints are enforced by WebFetchInputSchema.
func (i *WebFetchInput) UnmarshalJSON(data []byte) error {
	var aux struct {
		URL     string   `json:"url"`
		Format  *string  `json:"format"`
		Timeout *float64 `json:"timeout"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	format := "markdown"
	if aux.Format != nil {
		format = *aux.Format
	}
	*i = WebFetchInput{URL: aux.URL, Format: format, Timeout: aux.Timeout}
	return nil
}

type WebFetchOutput struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Format      string `json:"format"`
	Output      string `json:"output"`
}

const (
	webFetchBrowserUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36" //nolint:lll // canonical user-agent string
	webFetchFallbackUserAgent = "go-agent-sdk"
)

// WebFetchHTTPClient is the transport seam used by the tool, so callers can inject
// fakes in tests.
type WebFetchHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type WebFetchDeps struct {
	HTTPClient WebFetchHTTPClient
	Permission core.Permission
	// UserAgent is sent on the Cloudflare-challenge retry. Empty uses
	// webFetchFallbackUserAgent.
	UserAgent string
}

func webFetchAcceptHeader(format string) string {
	switch format {
	case "markdown":
		return "text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, text/html;q=0.7, */*;q=0.1"
	case "text":
		return "text/plain;q=1.0, text/markdown;q=0.9, text/html;q=0.8, */*;q=0.1"
	case "html":
		return "text/html;q=1.0, application/xhtml+xml;q=0.9, text/plain;q=0.8, text/markdown;q=0.7, */*;q=0.1"
	}
	return "*/*"
}

func webFetchHeaders(format, userAgent string) map[string]string {
	return map[string]string{
		"User-Agent":      userAgent,
		"Accept":          webFetchAcceptHeader(format),
		"Accept-Language": "en-US,en;q=0.9",
	}
}

func webFetchAssertHTTPURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL must use http:// or https://")
	}
	return nil
}

type webFetchStatusError struct {
	response *http.Response
}

func (e *webFetchStatusError) Error() string {
	return fmt.Sprintf("StatusCodeError: %d", e.response.StatusCode)
}

func webFetchIsCloudflareChallenge(err error) bool {
	var statusErr *webFetchStatusError
	if !errors.As(err, &statusErr) || statusErr.response == nil {
		return false
	}
	return statusErr.response.StatusCode == 403 &&
		statusErr.response.Header.Get("cf-mitigated") == "challenge"
}

func webFetchExecute(
	client WebFetchHTTPClient,
	ctx context.Context,
	rawURL, format, userAgent string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range webFetchHeaders(format, userAgent) {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, &webFetchStatusError{response: response}
	}
	return response, nil
}

func webFetchMimeFrom(contentType string) string {
	parts := strings.SplitN(contentType, ";", 2)
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func webFetchIsImageAttachment(mime string) bool {
	return strings.HasPrefix(mime, "image/") && mime != "image/svg+xml" && mime != "image/vnd.fastbidsheet"
}

func webFetchIsTextualMime(mime string) bool {
	return mime == "" ||
		strings.HasPrefix(mime, "text/") ||
		mime == "application/json" ||
		strings.HasSuffix(mime, "+json") ||
		mime == "application/xml" ||
		strings.HasSuffix(mime, "+xml") ||
		mime == "application/javascript" ||
		mime == "application/x-javascript"
}

func webFetchConvert(content, contentType, format string) (string, error) {
	if !strings.Contains(contentType, "text/html") {
		return content, nil
	}
	switch format {
	case "markdown":
		return ConvertHTMLToMarkdown(content)
	case "text":
		return ExtractTextFromHTML(content), nil
	}
	return content, nil
}

func webFetchFetch(deps WebFetchDeps, ctx context.Context, input WebFetchInput) ([]byte, string, error) {
	client := deps.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	fallbackUserAgent := deps.UserAgent
	if fallbackUserAgent == "" {
		fallbackUserAgent = webFetchFallbackUserAgent
	}
	response, err := webFetchExecute(client, ctx, input.URL, input.Format, webFetchBrowserUserAgent)
	if err != nil && webFetchIsCloudflareChallenge(err) {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		response, err = webFetchExecute(client, ctx, input.URL, input.Format, fallbackUserAgent)
	}
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, "", err
	}
	defer response.Body.Close()

	contentType := response.Header.Get("Content-Type")
	mime := webFetchMimeFrom(contentType)
	if webFetchIsImageAttachment(mime) {
		return nil, "", fmt.Errorf("Unsupported fetched image content type: %s", mime)
	}
	if !webFetchIsTextualMime(mime) {
		return nil, "", fmt.Errorf("Unsupported fetched file content type: %s", mime)
	}

	body, err := CollectBoundedResponseBody(response, WebFetchMaxResponseBytes, func() error {
		return fmt.Errorf("Response too large (exceeds %d byte limit)", WebFetchMaxResponseBytes)
	})
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}

func MakeWebFetch(deps WebFetchDeps) *Def[WebFetchInput, WebFetchOutput, WebFetchOutput] {
	return Make(Config[WebFetchInput, WebFetchOutput, WebFetchOutput]{
		Description: WebFetchDescription,
		Input:       WebFetchInputSchema,
		Output:      WebFetchOutputSchema,
		ToModelOutput: func(input WebFetchInput, output any) []Content {
			return []Content{TextContent{Text: DecodeStructured[WebFetchOutput](output).Output}}
		},
		Execute: func(execCtx context.Context, input WebFetchInput, ctx Context) (WebFetchOutput, error) {
			result, err := executeWebFetch(deps, execCtx, input, ctx)
			if err != nil {
				return WebFetchOutput{}, failure("Unable to fetch "+input.URL, err)
			}
			return result, nil
		},
	})
}

func executeWebFetch(
	deps WebFetchDeps,
	execCtx context.Context,
	input WebFetchInput,
	ctx Context,
) (WebFetchOutput, error) {
	if err := webFetchAssertHTTPURL(input.URL); err != nil {
		return WebFetchOutput{}, err
	}

	metadata := map[string]any{"url": input.URL, "format": input.Format}
	if input.Timeout != nil {
		metadata["timeout"] = *input.Timeout
	}
	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}
	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    WebFetchName,
		Resources: []string{input.URL},
		Save:      []string{"*"},
		Metadata:  metadata,
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return WebFetchOutput{}, err
	}

	timeoutSeconds := float64(WebFetchDefaultTimeoutSeconds)
	if input.Timeout != nil {
		timeoutSeconds = *input.Timeout
	}
	timeoutCtx, cancel := context.WithTimeout(execCtx, time.Duration(timeoutSeconds*float64(time.Second)))
	defer cancel()

	body, contentType, err := webFetchFetch(deps, timeoutCtx, input)
	if err != nil {
		return WebFetchOutput{}, err
	}

	output, err := webFetchConvert(string(body), contentType, input.Format)
	if err != nil {
		return WebFetchOutput{}, err
	}
	return WebFetchOutput{
		URL:         input.URL,
		ContentType: contentType,
		Format:      input.Format,
		Output:      output,
	}, nil
}

func RegisterWebFetch(tools Tools, deps WebFetchDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		WebFetchName: MakeWebFetch(deps),
	})
}

var webFetchSkipTextTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "iframe": true, "object": true, "embed": true,
}

var webFetchVoidTags = map[string]bool{
	"area": true, "base": true, "basefont": true, "br": true, "col": true, "command": true,
	"embed": true, "frame": true, "hr": true, "img": true, "input": true, "isindex": true,
	"keygen": true, "link": true, "meta": true, "param": true, "source": true, "track": true,
	"wbr": true,
}

// ExtractTextFromHTML returns the concatenated text of an HTML document while
// dropping the contents of script/style/noscript/iframe/object/embed elements.
// Nested markup inside <iframe> is not tokenized, since the HTML spec treats
// iframe content as raw text.
func ExtractTextFromHTML(input string) string {
	var b strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(input))
	skipDepth := 0
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)
			if webFetchVoidTags[tag] {
				continue
			}
			if skipDepth > 0 || webFetchSkipTextTags[tag] {
				skipDepth++
			}
		case html.TextToken:
			if skipDepth == 0 {
				b.Write(tokenizer.Text())
			}
		case html.EndTagToken:
			if skipDepth > 0 {
				skipDepth--
			}
		}
	}
	return turndownTrimSpace(b.String())
}

// ConvertHTMLToMarkdown converts HTML to Markdown with the default turndown
// options.
func ConvertHTMLToMarkdown(input string) (string, error) {
	options := defaultTurndownOptions()
	options.headingStyle = "atx"
	options.hr = "---"
	options.bulletListMarker = "-"
	options.codeBlockStyle = "fenced"
	options.emDelimiter = "*"
	service := newTurndownService(options)
	service.removeTags("script", "style", "meta", "link")
	return service.turndown(input)
}
