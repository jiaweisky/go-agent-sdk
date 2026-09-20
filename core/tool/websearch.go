package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/flag"
	"github.com/user/go-agent-sdk/core/installation"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/jsonschema"
)

const WebSearchName = "websearch"

const (
	WebSearchNoResults            = "No search results found. Please try a different query."
	WebSearchExaURL               = "https://mcp.exa.ai/mcp"
	WebSearchParallelURL          = "https://search.parallel.ai/mcp"
	WebSearchMaxNumResults        = 20
	WebSearchMaxContextCharacters = 50_000
	WebSearchMaxResponseBytes     = 256 * 1024
)

// WebSearchDescription ports websearch.ts description. It embeds the current
// year at package initialization, mirroring the TS module-level template.
var WebSearchDescription = fmt.Sprintf(`Search the web using the session's local web search provider. Use this for current information beyond knowledge cutoff.

This is a provider-independent local tool backed by Exa or Parallel. Provider-hosted web search tools are separate and execute at the model provider.

Optional controls support result count, live crawling ('fallback' or 'preferred'), search type ('auto', 'fast', or 'deep'), and maximum context characters.

The current year is %d. Use this year when searching for recent information or current events.`, time.Now().Year())

var WebSearchInputSchema = jsonschema.Object(
	jsonschema.Required("query", jsonschema.String(jsonschema.Description("Websearch query"))),
	jsonschema.Optional("numResults", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Maximum(WebSearchMaxNumResults),
		jsonschema.Description(fmt.Sprintf("Number of search results to return (default: 8, maximum: %d)", WebSearchMaxNumResults)),
	)),
	jsonschema.Optional("livecrawl", jsonschema.Enum(
		[]any{"fallback", "preferred"},
		jsonschema.Description("Live crawl mode - 'fallback': use live crawling as backup if cached unavailable, "+
			"'preferred': prioritize live crawling (default: 'fallback')"),
	)),
	jsonschema.Optional("type", jsonschema.Enum(
		[]any{"auto", "fast", "deep"},
		jsonschema.Description("Search type - 'auto': balanced search (default), 'fast': quick results, "+
			"'deep': comprehensive search"),
	)),
	jsonschema.Optional("contextMaxCharacters", jsonschema.Integer(
		jsonschema.ExclusiveMinimum(0),
		jsonschema.Maximum(WebSearchMaxContextCharacters),
		jsonschema.Description(fmt.Sprintf(
			"Maximum characters for context string optimized for models (default: 10000, maximum: %d)",
			WebSearchMaxContextCharacters,
		)),
	)),
)

var WebSearchOutputSchema = jsonschema.Object(
	jsonschema.Required("provider", jsonschema.Literal("exa", "parallel")),
	jsonschema.Required("text", jsonschema.String()),
)

type WebSearchInput struct {
	Query                string `json:"query"`
	NumResults           *int   `json:"numResults,omitempty"`
	Livecrawl            string `json:"livecrawl,omitempty"`
	Type                 string `json:"type,omitempty"`
	ContextMaxCharacters *int   `json:"contextMaxCharacters,omitempty"`
}

type WebSearchProvider string

const (
	WebSearchProviderExa      WebSearchProvider = "exa"
	WebSearchProviderParallel WebSearchProvider = "parallel"
)

type WebSearchConfig struct {
	Provider       *WebSearchProvider
	EnableExa      bool
	EnableParallel bool
	ExaAPIKey      string
	ParallelAPIKey string
}

// DefaultWebSearchConfig ports websearch.ts defaultConfigLayer. The product
// environment contract is isolated here from the generic tool implementation.
func DefaultWebSearchConfig() WebSearchConfig {
	config := WebSearchConfig{
		EnableExa: flag.Truthy("OPENCODE_EXPERIMENTAL") ||
			flag.Truthy("OPENCODE_ENABLE_EXA") ||
			flag.Truthy("OPENCODE_EXPERIMENTAL_EXA"),
		EnableParallel: flag.Truthy("OPENCODE_ENABLE_PARALLEL") || flag.Truthy("OPENCODE_EXPERIMENTAL_PARALLEL"),
		ExaAPIKey:      os.Getenv("EXA_API_KEY"),
		ParallelAPIKey: os.Getenv("PARALLEL_API_KEY"),
	}
	switch os.Getenv("OPENCODE_WEBSEARCH_PROVIDER") {
	case string(WebSearchProviderExa):
		provider := WebSearchProviderExa
		config.Provider = &provider
	case string(WebSearchProviderParallel):
		provider := WebSearchProviderParallel
		config.Provider = &provider
	}
	return config
}

// SelectWebSearchProvider ports selectProvider. Without an override it prefers
// an explicitly enabled provider, otherwise it chooses deterministically from
// the session ID checksum.
func SelectWebSearchProvider(sessionID string, config WebSearchConfig, override *WebSearchProvider) WebSearchProvider {
	if override != nil {
		return *override
	}
	if config.EnableParallel {
		return WebSearchProviderParallel
	}
	if config.EnableExa {
		return WebSearchProviderExa
	}
	value := int64(0)
	if checksum := util.Checksum(sessionID); checksum != "" {
		if parsed, err := strconv.ParseInt(checksum, 36, 64); err == nil {
			value = parsed
		}
	}
	if value%2 == 0 {
		return WebSearchProviderExa
	}
	return WebSearchProviderParallel
}

type webSearchMcpContent struct {
	Type *string `json:"type"`
	Text *string `json:"text"`
}

type webSearchMcpResult struct {
	Result *struct {
		Content *[]webSearchMcpContent `json:"content"`
	} `json:"result"`
}

func webSearchParsePayload(payload string) (string, error) {
	trimmed := strings.TrimSpace(payload)
	if !strings.HasPrefix(trimmed, "{") {
		return "", nil
	}
	var decoded webSearchMcpResult
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return "", err
	}
	if decoded.Result == nil || decoded.Result.Content == nil {
		return "", errors.New("invalid MCP search response")
	}
	for _, item := range *decoded.Result.Content {
		if item.Type == nil || item.Text == nil {
			return "", errors.New("invalid MCP search response")
		}
		if *item.Text != "" {
			return *item.Text, nil
		}
	}
	return "", nil
}

// ParseWebSearchResponse ports parseResponse: it accepts either a direct JSON
// body or an SSE stream of `data: ` lines and returns the first text payload.
func ParseWebSearchResponse(body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed != "" {
		direct, err := webSearchParsePayload(trimmed)
		if err != nil {
			return "", err
		}
		if direct != "" {
			return direct, nil
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data, err := webSearchParsePayload(line[len("data: "):])
		if err != nil {
			return "", err
		}
		if data != "" {
			return data, nil
		}
	}
	return "", nil
}

type webSearchExaArgs struct {
	Query                string `json:"query"`
	Type                 string `json:"type"`
	NumResults           int    `json:"numResults"`
	Livecrawl            string `json:"livecrawl"`
	ContextMaxCharacters *int   `json:"contextMaxCharacters,omitempty"`
}

type webSearchParallelArgs struct {
	Objective     string   `json:"objective"`
	SearchQueries []string `json:"search_queries"`
	SessionID     string   `json:"session_id"`
}

type webSearchMcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
	} `json:"params"`
}

func webSearchExaURL(apiKey string) string {
	if apiKey == "" {
		return WebSearchExaURL
	}
	parsed, err := url.Parse(WebSearchExaURL)
	if err != nil {
		return WebSearchExaURL
	}
	query := parsed.Query()
	query.Set("exaApiKey", apiKey)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// WebSearchHTTPClient is the transport seam used by the tool. It mirrors the
// Effect HttpClient capability so callers can inject fakes in tests.
type WebSearchHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

const webSearchTimeout = 25 * time.Second

func webSearchCallMCP(
	ctx context.Context,
	client WebSearchHTTPClient,
	rawURL string,
	toolName string,
	args any,
	headers map[string]string,
) (string, error) {
	request := webSearchMcpRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call"}
	request.Params.Name = toolName
	request.Params.Arguments = args
	payload, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s request timed out", toolName)
		}
		return "", err
	}
	if response.Body != nil {
		defer response.Body.Close()
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("StatusCodeError: %d", response.StatusCode)
	}

	body, err := CollectBoundedResponseBody(response, WebSearchMaxResponseBytes, func() error {
		return fmt.Errorf("%s response exceeded %d bytes", toolName, WebSearchMaxResponseBytes)
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s request timed out", toolName)
		}
		return "", err
	}
	return ParseWebSearchResponse(string(body))
}

type WebSearchOutput struct {
	Provider WebSearchProvider `json:"provider"`
	Text     string            `json:"text"`
}

type WebSearchDeps struct {
	HTTPClient WebSearchHTTPClient
	Permission core.Permission
	Config     WebSearchConfig
}

func webSearchMetadata(input WebSearchInput, provider WebSearchProvider) map[string]any {
	metadata := map[string]any{"query": input.Query, "provider": string(provider)}
	if input.NumResults != nil {
		metadata["numResults"] = *input.NumResults
	}
	if input.Livecrawl != "" {
		metadata["livecrawl"] = input.Livecrawl
	}
	if input.Type != "" {
		metadata["type"] = input.Type
	}
	if input.ContextMaxCharacters != nil {
		metadata["contextMaxCharacters"] = *input.ContextMaxCharacters
	}
	return metadata
}

func executeWebSearch(deps WebSearchDeps, execCtx context.Context, input WebSearchInput, ctx Context) (WebSearchOutput, error) {
	provider := SelectWebSearchProvider(ctx.SessionID, deps.Config, deps.Config.Provider)

	source := &core.PermissionSource{
		Type:      core.PermissionSourceTypeTool,
		MessageID: ctx.AssistantMessageID,
		CallID:    ctx.ToolCallID,
	}
	if err := deps.Permission.Assert(core.PermissionAssertInput{
		SessionID: ctx.SessionID,
		Action:    WebSearchName,
		Resources: []string{input.Query},
		Save:      []string{"*"},
		Metadata:  webSearchMetadata(input, provider),
		Source:    source,
		Agent:     &ctx.Agent,
	}); err != nil {
		return WebSearchOutput{}, err
	}

	client := deps.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	var text string
	var err error
	if provider == WebSearchProviderExa {
		searchType := input.Type
		if searchType == "" {
			searchType = "auto"
		}
		numResults := 8
		if input.NumResults != nil {
			numResults = *input.NumResults
		}
		livecrawl := input.Livecrawl
		if livecrawl == "" {
			livecrawl = "fallback"
		}
		text, err = webSearchCallMCP(execCtx, client, webSearchExaURL(deps.Config.ExaAPIKey), "web_search_exa", webSearchExaArgs{
			Query:                input.Query,
			Type:                 searchType,
			NumResults:           numResults,
			Livecrawl:            livecrawl,
			ContextMaxCharacters: input.ContextMaxCharacters,
		}, nil)
	} else {
		headers := map[string]string{"User-Agent": "opencode/" + installation.InstallationVersion}
		if deps.Config.ParallelAPIKey != "" {
			headers["Authorization"] = "Bearer " + deps.Config.ParallelAPIKey
		}
		text, err = webSearchCallMCP(execCtx, client, WebSearchParallelURL, "web_search", webSearchParallelArgs{
			Objective:     input.Query,
			SearchQueries: []string{input.Query},
			SessionID:     ctx.SessionID,
		}, headers)
	}
	if err != nil {
		return WebSearchOutput{}, err
	}
	if text == "" {
		text = WebSearchNoResults
	}
	return WebSearchOutput{Provider: provider, Text: text}, nil
}

func MakeWebSearch(deps WebSearchDeps) *Def[WebSearchInput, WebSearchOutput, WebSearchOutput] {
	return Make(Config[WebSearchInput, WebSearchOutput, WebSearchOutput]{
		Description: WebSearchDescription,
		Input:       WebSearchInputSchema,
		Output:      WebSearchOutputSchema,
		ToModelOutput: func(input WebSearchInput, output any) []Content {
			var out WebSearchOutput
			if b, err := json.Marshal(output); err == nil {
				json.Unmarshal(b, &out)
			}
			return []Content{TextContent{Text: out.Text}}
		},
		Execute: func(execCtx context.Context, input WebSearchInput, ctx Context) (WebSearchOutput, error) {
			result, err := executeWebSearch(deps, execCtx, input, ctx)
			if err != nil {
				return WebSearchOutput{}, &Failure{Message: "Unable to search the web for " + input.Query}
			}
			return result, nil
		},
	})
}

func RegisterWebSearch(registry *Registry, deps WebSearchDeps) (func(), error) {
	return registry.Register(map[string]Tool{
		WebSearchName: MakeWebSearch(deps),
	})
}
