package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/core/installation"
	"github.com/user/go-agent-sdk/llm/schema"
)

func webSearchResultText(t *testing.T, result *Settlement) string {
	t.Helper()
	text, ok := result.Result.Value.(string)
	if !ok {
		t.Fatalf("expected text result, got %#v", result.Result.Value)
	}
	return text
}

func webSearchPayload(text string) string {
	encoded, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}},
	})
	return string(encoded)
}

type webSearchTestClient struct {
	requests []*http.Request
	bodies   []string
	response func() (*http.Response, error)
}

func (c *webSearchTestClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		c.bodies = append(c.bodies, string(body))
	}
	if c.response != nil {
		return c.response()
	}
	return nil, errors.New("no scripted response")
}

func webSearchTextResponse(body string) func() (*http.Response, error) {
	return func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
}

func settleWebSearch(t *testing.T, deps WebSearchDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterWebSearch(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_websearch_test",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call-websearch",
			Name:  WebSearchName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestWebSearch_ProviderSelection(t *testing.T) {
	sessionID := "ses_websearch_test"
	if SelectWebSearchProvider(sessionID, WebSearchConfig{}, nil) != SelectWebSearchProvider(sessionID, WebSearchConfig{}, nil) {
		t.Fatalf("expected stable provider per session")
	}

	parallel := WebSearchProviderParallel
	if got := SelectWebSearchProvider(sessionID, WebSearchConfig{}, &parallel); got != WebSearchProviderParallel {
		t.Fatalf("override parallel = %q", got)
	}
	exa := WebSearchProviderExa
	if got := SelectWebSearchProvider(sessionID, WebSearchConfig{}, &exa); got != WebSearchProviderExa {
		t.Fatalf("override exa = %q", got)
	}
	if got := SelectWebSearchProvider(sessionID, WebSearchConfig{EnableExa: true, EnableParallel: true}, nil); got != WebSearchProviderParallel {
		t.Fatalf("both flags = %q", got)
	}
	if got := SelectWebSearchProvider(sessionID, WebSearchConfig{EnableExa: true}, nil); got != WebSearchProviderExa {
		t.Fatalf("exa flag = %q", got)
	}
}

func TestWebSearch_RejectsOutOfRangeNumericControls(t *testing.T) {
	var input WebSearchInput
	if err := decodeAndValidate(map[string]any{"query": "x", "numResults": 0}, &input, compileSchema(WebSearchInputSchema)); err == nil {
		t.Fatalf("expected numResults 0 to be rejected")
	}
	if err := decodeAndValidate(map[string]any{"query": "x", "numResults": WebSearchMaxNumResults + 1}, &input, compileSchema(WebSearchInputSchema)); err == nil {
		t.Fatalf("expected numResults above maximum to be rejected")
	}
	if err := decodeAndValidate(map[string]any{"query": "x", "contextMaxCharacters": WebSearchMaxContextCharacters + 1}, &input, compileSchema(WebSearchInputSchema)); err == nil {
		t.Fatalf("expected contextMaxCharacters above maximum to be rejected")
	}
}

func TestWebSearch_ParsesPlainAndSSEResponses(t *testing.T) {
	if got, err := ParseWebSearchResponse(webSearchPayload("search results")); err != nil || got != "search results" {
		t.Fatalf("plain parse = %q, %v", got, err)
	}
	sse := "data: [DONE]\nevent: message\ndata: " + webSearchPayload("search results") + "\n\n"
	if got, err := ParseWebSearchResponse(sse); err != nil || got != "search results" {
		t.Fatalf("sse parse = %q, %v", got, err)
	}
}

func TestWebSearch_ParseRejectsMalformedPayloads(t *testing.T) {
	if _, err := ParseWebSearchResponse(`{"jsonrpc":"2.0","id":1,"error":{"code":-1}}`); err == nil {
		t.Fatalf("expected error for JSON-RPC error response")
	}
	if _, err := ParseWebSearchResponse(`{"result":{"content":[{"type":"text"}]}}`); err == nil {
		t.Fatalf("expected error for content item missing text")
	}
	if _, err := ParseWebSearchResponse(`{"result":{}}`); err == nil {
		t.Fatalf("expected error for result missing content")
	}
}

func TestWebSearch_CallsExaAndAssertsPermission(t *testing.T) {
	client := &webSearchTestClient{response: webSearchTextResponse(webSearchPayload("exa results"))}
	permission := &bashTestPermission{}
	deps := WebSearchDeps{
		HTTPClient: client,
		Permission: permission,
		Config:     WebSearchConfig{Provider: webSearchProviderPtr(WebSearchProviderExa)},
	}

	result := settleWebSearch(t, deps, map[string]any{
		"query":                "effect typescript",
		"numResults":           3,
		"livecrawl":            "preferred",
		"type":                 "fast",
		"contextMaxCharacters": 2500,
	})
	if result.Result.Type != "text" || result.Result.Value != "exa results" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if len(permission.assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(permission.assertions))
	}
	assertion := permission.assertions[0]
	if assertion.Action != WebSearchName || len(assertion.Resources) != 1 || assertion.Resources[0] != "effect typescript" {
		t.Fatalf("unexpected assertion: %+v", assertion)
	}
	if len(assertion.Save) != 1 || assertion.Save[0] != "*" {
		t.Fatalf("unexpected save: %+v", assertion.Save)
	}
	expectedMetadata := map[string]any{
		"query":                "effect typescript",
		"numResults":           3,
		"livecrawl":            "preferred",
		"type":                 "fast",
		"contextMaxCharacters": 2500,
		"provider":             "exa",
	}
	for key, want := range expectedMetadata {
		if got := assertion.Metadata[key]; got != want {
			t.Fatalf("metadata[%q] = %#v, want %#v", key, got, want)
		}
	}

	if len(client.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(client.requests))
	}
	if got := client.requests[0].URL.String(); got != WebSearchExaURL {
		t.Fatalf("exa url = %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(client.bodies[0]), &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	params := body["params"].(map[string]any)
	if params["name"] != "web_search_exa" {
		t.Fatalf("tool name = %v", params["name"])
	}
	args := params["arguments"].(map[string]any)
	if args["query"] != "effect typescript" || args["type"] != "fast" ||
		args["numResults"] != float64(3) || args["livecrawl"] != "preferred" ||
		args["contextMaxCharacters"] != float64(2500) {
		t.Fatalf("unexpected exa args: %+v", args)
	}
}

func TestWebSearch_CallsParallelWithBearer(t *testing.T) {
	client := &webSearchTestClient{response: webSearchTextResponse(webSearchPayload("parallel results"))}
	deps := WebSearchDeps{
		HTTPClient: client,
		Permission: &bashTestPermission{},
		Config: WebSearchConfig{
			Provider:       webSearchProviderPtr(WebSearchProviderParallel),
			ParallelAPIKey: "parallel-secret",
		},
	}

	result := settleWebSearch(t, deps, map[string]any{"query": "effect layers"})
	if result.Result.Type != "text" || result.Result.Value != "parallel results" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if result.Output == nil {
		t.Fatalf("expected output")
	}
	if strings.Contains(webSearchResultText(t, result), "parallel-secret") {
		t.Fatalf("credential leaked into result")
	}

	if got := client.requests[0].URL.String(); got != WebSearchParallelURL {
		t.Fatalf("parallel url = %q", got)
	}
	if got := client.requests[0].Header.Get("Authorization"); got != "Bearer parallel-secret" {
		t.Fatalf("authorization = %q", got)
	}
	if got := client.requests[0].Header.Get("User-Agent"); got != "opencode/"+installation.InstallationVersion {
		t.Fatalf("user agent = %q", got)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(client.bodies[0]), &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	params := body["params"].(map[string]any)
	args := params["arguments"].(map[string]any)
	if params["name"] != "web_search" || args["objective"] != "effect layers" ||
		args["session_id"] != "ses_websearch_test" {
		t.Fatalf("unexpected parallel args: %+v", args)
	}
	queries := args["search_queries"].([]any)
	if len(queries) != 1 || queries[0] != "effect layers" {
		t.Fatalf("unexpected search_queries: %+v", queries)
	}
}

func TestWebSearch_KeepsExaCredentialInURL(t *testing.T) {
	client := &webSearchTestClient{response: webSearchTextResponse(webSearchPayload("credentialed exa results"))}
	deps := WebSearchDeps{
		HTTPClient: client,
		Permission: &bashTestPermission{},
		Config: WebSearchConfig{
			Provider:  webSearchProviderPtr(WebSearchProviderExa),
			ExaAPIKey: "exa secret",
		},
	}

	result := settleWebSearch(t, deps, map[string]any{"query": "effect schema"})
	if result.Result.Value != "credentialed exa results" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
	if strings.Contains(webSearchResultText(t, result), "exa secret") {
		t.Fatalf("credential leaked into result")
	}
	if got := client.requests[0].URL.String(); got != WebSearchExaURL+"?exaApiKey=exa+secret" {
		t.Fatalf("exa url = %q", got)
	}
}

func TestWebSearch_ReturnsNoResultsFallback(t *testing.T) {
	client := &webSearchTestClient{response: webSearchTextResponse("")}
	deps := WebSearchDeps{
		HTTPClient: client,
		Permission: &bashTestPermission{},
		Config:     WebSearchConfig{Provider: webSearchProviderPtr(WebSearchProviderExa)},
	}
	result := settleWebSearch(t, deps, map[string]any{"query": "nothing"})
	if result.Result.Type != "text" || result.Result.Value != WebSearchNoResults {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
}

func TestWebSearch_RejectsOversizedResponse(t *testing.T) {
	client := &webSearchTestClient{response: func() (*http.Response, error) {
		header := http.Header{"Content-Length": []string{"999999999"}}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}}
	deps := WebSearchDeps{
		HTTPClient: client,
		Permission: &bashTestPermission{},
		Config:     WebSearchConfig{Provider: webSearchProviderPtr(WebSearchProviderExa)},
	}
	result := settleWebSearch(t, deps, map[string]any{"query": "too much"})
	if result.Result.Type != "error" || result.Result.Value != "Unable to search the web for too much" {
		t.Fatalf("unexpected result: %+v", result.Result)
	}
}

func webSearchProviderPtr(provider WebSearchProvider) *WebSearchProvider {
	return &provider
}
