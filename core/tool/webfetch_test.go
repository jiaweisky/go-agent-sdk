package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/user/go-agent-sdk/llm/schema"
)

type webFetchTestClient struct {
	responses []func(*http.Request) (*http.Response, error)
	requests  []*http.Request
}

func (c *webFetchTestClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.responses) == 0 {
		return nil, errors.New("no scripted response")
	}
	next := c.responses[0]
	if len(c.responses) > 1 {
		c.responses = c.responses[1:]
	}
	return next(req)
}

func webFetchResponse(body string, status int, headers map[string]string) *http.Response {
	header := http.Header{}
	for key, value := range headers {
		header.Set(key, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func webFetchTextResponse(body string, contentType string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		return webFetchResponse(body, http.StatusOK, map[string]string{"content-type": contentType}), nil
	}
}

func settleWebFetch(t *testing.T, deps WebFetchDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterWebFetch(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_webfetch_test",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call-webfetch",
			Name:  WebFetchName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestWebFetch_DefaultsFormatAndValidatesTimeout(t *testing.T) {
	var input WebFetchInput
	if err := json.Unmarshal([]byte(`{"url":"https://example.com"}`), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if input.Format != "markdown" {
		t.Fatalf("expected default format markdown, got %q", input.Format)
	}

	if err := decodeAndValidate(map[string]any{"url": "https://example.com", "timeout": 0}, &input, compileSchema(WebFetchInputSchema.Validation())); err == nil {
		t.Fatalf("expected timeout 0 to be rejected")
	}
	if err := decodeAndValidate(map[string]any{"url": "https://example.com", "timeout": WebFetchMaxTimeoutSeconds + 1}, &input, compileSchema(WebFetchInputSchema.Validation())); err == nil {
		t.Fatalf("expected timeout above maximum to be rejected")
	}
	if err := decodeAndValidate(map[string]any{"url": "https://example.com", "timeout": 1.5}, &input, compileSchema(WebFetchInputSchema.Validation())); err != nil {
		t.Fatalf("expected fractional timeout to be accepted: %v", err)
	}
	if input.Timeout == nil || *input.Timeout != 1.5 {
		t.Fatalf("expected fractional timeout 1.5, got %v", input.Timeout)
	}
}

func TestWebFetch_Helpers(t *testing.T) {
	html := "<h1>Hello</h1><script>bad()</script><p>world <strong>wide</strong></p><style>.bad {}</style>"
	if got := ExtractTextFromHTML(html); got != "Helloworld wide" {
		t.Fatalf("extractTextFromHTML = %q", got)
	}
	markdown, err := ConvertHTMLToMarkdown(html)
	if err != nil {
		t.Fatalf("convertHTMLToMarkdown: %v", err)
	}
	if markdown != "# Hello\n\nworld **wide**" {
		t.Fatalf("convertHTMLToMarkdown = %q", markdown)
	}
}

func TestWebFetch_FetchesAndAssertsPermission(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		webFetchTextResponse("hello", "text/plain"),
	}}
	permission := &bashTestPermission{}
	deps := WebFetchDeps{HTTPClient: client, Permission: permission}
	url := "http://example.com/public"

	result := settleWebFetch(t, deps, map[string]any{"url": url, "format": "text", "timeout": 4})
	if result.Result.Type != "text" || result.Result.Value != "hello" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if len(permission.assertions) != 1 {
		t.Fatalf("expected 1 permission assertion, got %d", len(permission.assertions))
	}
	assertion := permission.assertions[0]
	if assertion.Action != WebFetchName || len(assertion.Resources) != 1 || assertion.Resources[0] != url {
		t.Fatalf("unexpected permission assertion: %+v", assertion)
	}
	if len(assertion.Save) != 1 || assertion.Save[0] != "*" {
		t.Fatalf("unexpected save rules: %+v", assertion.Save)
	}
	if assertion.Metadata["url"] != url || assertion.Metadata["format"] != "text" || assertion.Metadata["timeout"] != float64(4) {
		t.Fatalf("unexpected metadata: %+v", assertion.Metadata)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(client.requests))
	}
	if got := client.requests[0].URL.String(); got != url {
		t.Fatalf("expected request URL %q without rewriting, got %q", url, got)
	}
	if got := client.requests[0].Header.Get("Accept"); !strings.Contains(got, "text/plain;q=1.0") {
		t.Fatalf("unexpected accept header: %q", got)
	}
	if got := client.requests[0].Header.Get("User-Agent"); !strings.Contains(got, "Mozilla/5.0") {
		t.Fatalf("unexpected user agent: %q", got)
	}
}

func TestWebFetch_AcceptsLocalhost(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		webFetchTextResponse("hello", "text/plain"),
	}}
	permission := &bashTestPermission{}
	url := "http://localhost/private"

	result := settleWebFetch(t, WebFetchDeps{HTTPClient: client, Permission: permission}, map[string]any{
		"url": url, "format": "text",
	})
	if result.Result.Type != "text" || result.Result.Value != "hello" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if len(permission.assertions) != 1 || permission.assertions[0].Resources[0] != url {
		t.Fatalf("unexpected permission assertions: %+v", permission.assertions)
	}
}

func TestWebFetch_FollowsRedirectsApprovingOnlyRequestedURL(t *testing.T) {
	target := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		target = r.URL.String()
		w.Header().Set("content-type", "text/plain")
		_, _ = w.Write([]byte("redirected"))
	}))
	defer server.Close()

	permission := &bashTestPermission{}
	url := server.URL + "/redirect"
	result := settleWebFetch(t, WebFetchDeps{Permission: permission}, map[string]any{"url": url, "format": "text"})
	if result.Result.Type != "text" || result.Result.Value != "redirected" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if !strings.HasSuffix(target, "/target") {
		t.Fatalf("expected redirect to /target, got %q", target)
	}
	if len(permission.assertions) != 1 || permission.assertions[0].Resources[0] != url {
		t.Fatalf("expected approval only for requested URL, got %+v", permission.assertions)
	}
}

func TestWebFetch_RejectsNonHTTPScheme(t *testing.T) {
	client := &webFetchTestClient{}
	permission := &bashTestPermission{}
	result := settleWebFetch(t, WebFetchDeps{HTTPClient: client, Permission: permission}, map[string]any{
		"url": "file:///etc/passwd", "format": "text",
	})
	if result.Result.Type != "error" || result.Result.Value != "Unable to fetch file:///etc/passwd" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if len(permission.assertions) != 0 {
		t.Fatalf("expected no permission assertions, got %d", len(permission.assertions))
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected no requests, got %d", len(client.requests))
	}
}

func TestWebFetch_ConvertsHTML(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		webFetchTextResponse("<h1>Hello</h1><p>world</p><script>bad()</script>", "text/html; charset=utf-8"),
		webFetchTextResponse("<h1>Hello</h1><p>world</p><script>bad()</script>", "text/html; charset=utf-8"),
	}}
	deps := WebFetchDeps{HTTPClient: client, Permission: &bashTestPermission{}}

	markdown := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1", "format": "markdown"})
	if markdown.Result.Value != "# Hello\n\nworld" {
		t.Fatalf("unexpected markdown: %v", markdown.Result.Value)
	}
	text := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1", "format": "text"})
	if text.Result.Value != "Helloworld" {
		t.Fatalf("unexpected text: %v", text.Result.Value)
	}
}

func TestWebFetch_RejectsOversizedBodies(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return webFetchResponse("small", http.StatusOK, map[string]string{
				"content-type":   "text/plain",
				"content-length": strconv.Itoa(WebFetchMaxResponseBytes + 1),
			}), nil
		},
		func(*http.Request) (*http.Response, error) {
			return webFetchResponse(strings.Repeat("x", WebFetchMaxResponseBytes+1), http.StatusOK, map[string]string{
				"content-type": "text/plain",
			}), nil
		},
	}}
	deps := WebFetchDeps{HTTPClient: client, Permission: &bashTestPermission{}}

	declared := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1/declared", "format": "text"})
	if declared.Result.Type != "error" || declared.Result.Value != "Unable to fetch https://1.1.1.1/declared" {
		t.Fatalf("unexpected declared result: %+v", declared.Result)
	}
	streamed := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1/streamed", "format": "text"})
	if streamed.Result.Type != "error" || streamed.Result.Value != "Unable to fetch https://1.1.1.1/streamed" {
		t.Fatalf("unexpected streamed result: %+v", streamed.Result)
	}
}

func TestWebFetch_RejectsImagesAndFiles(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		webFetchTextResponse("png", "image/png"),
		webFetchTextResponse("pdf", "application/pdf"),
	}}
	deps := WebFetchDeps{HTTPClient: client, Permission: &bashTestPermission{}}

	image := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1/image", "format": "html"})
	if image.Result.Type != "error" || image.Result.Value != "Unable to fetch https://1.1.1.1/image" {
		t.Fatalf("unexpected image result: %+v", image.Result)
	}
	file := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1/file", "format": "html"})
	if file.Result.Type != "error" || file.Result.Value != "Unable to fetch https://1.1.1.1/file" {
		t.Fatalf("unexpected file result: %+v", file.Result)
	}
}

func TestWebFetch_RetriesCloudflareChallenge(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return webFetchResponse("challenge", http.StatusForbidden, map[string]string{"cf-mitigated": "challenge"}), nil
		},
		webFetchTextResponse("ok", "text/plain"),
	}}
	deps := WebFetchDeps{HTTPClient: client, Permission: &bashTestPermission{}}

	result := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1", "format": "text"})
	if result.Result.Type != "text" || result.Result.Value != "ok" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(client.requests))
	}
	if ua := client.requests[0].Header.Get("User-Agent"); !strings.Contains(ua, "Mozilla/5.0") {
		t.Fatalf("unexpected first user agent: %q", ua)
	}
	if ua := client.requests[1].Header.Get("User-Agent"); ua != "opencode" {
		t.Fatalf("unexpected second user agent: %q", ua)
	}
}

func TestWebFetch_TimesOut(t *testing.T) {
	client := &webFetchTestClient{responses: []func(*http.Request) (*http.Response, error){
		func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		},
	}}
	deps := WebFetchDeps{HTTPClient: client, Permission: &bashTestPermission{}}

	start := time.Now()
	result := settleWebFetch(t, deps, map[string]any{"url": "https://1.1.1.1/slow", "format": "text", "timeout": 1})
	if result.Result.Type != "error" || result.Result.Value != "Unable to fetch https://1.1.1.1/slow" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
}

// TestWebFetch_TurndownParity pins convertHTMLToMarkdown against the upstream
// turndown 7.2.0 configured exactly like the TS service. Expectations were
// generated by running that library over the same fixtures.
func TestWebFetch_TurndownParity(t *testing.T) {
	cases := []struct {
		html     string
		markdown string
	}{
		{`<h1>Hello</h1><script>bad()</script><p>world <strong>wide</strong></p><style>.bad {}</style>`, "# Hello\n\nworld **wide**"},
		{`<ul><li>a</li><li>b</li></ul>`, "-   a\n-   b"},
		{`<ol><li>one</li><li>two</li></ol>`, "1.  one\n2.  two"},
		{"<pre><code>const x = 1;\n</code></pre>", "```\nconst x = 1;\n```"},
		{`<p>see <a href="https://x.test">link</a></p>`, "see [link](https://x.test)"},
		{`<blockquote><p>quoted</p></blockquote>`, "> quoted"},
		{`<table><tr><th>h</th></tr><tr><td>c</td></tr></table>`, "h\n\nc"},
		{`<ul><li>a<ul><li>a1</li></ul></li></ul>`, "-   a\n    -   a1"},
		{`<h2>Sub</h2><h3>Deep</h3>`, "## Sub\n\n### Deep"},
		{`<p>a<br>b</p>`, "a  \nb"},
		{`<hr>`, "---"},
		{"<p><em>em</em> and <code>inline()</code></p>", "*em* and `inline()`"},
		{`<p><img src="/a.png" alt="pic"></p>`, "![pic](/a.png)"},
		{`<blockquote><blockquote><p>deep</p></blockquote></blockquote>`, "> > deep"},
		{`<ol start="3"><li>x</li></ol>`, "3.  x"},
		{`<p>plain</p><div>block</div>`, "plain\n\nblock"},
	}
	for _, tc := range cases {
		got, err := ConvertHTMLToMarkdown(tc.html)
		if err != nil {
			t.Fatalf("ConvertHTMLToMarkdown(%s): %v", tc.html, err)
		}
		if got != tc.markdown {
			t.Fatalf("ConvertHTMLToMarkdown(%s) = %q, want %q", tc.html, got, tc.markdown)
		}
	}
}

// TestWebFetch_ExtractTextParity mirrors packages/core/test/tool-webfetch.test.ts
// "ports HTML text and markdown conversions without active content".
func TestWebFetch_ExtractTextParity(t *testing.T) {
	html := "<h1>Hello</h1><script>bad()</script><p>world <strong>wide</strong></p><style>.bad {}</style>"
	if got := ExtractTextFromHTML(html); got != "Helloworld wide" {
		t.Fatalf("ExtractTextFromHTML = %q, want %q", got, "Helloworld wide")
	}
}
