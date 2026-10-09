package runner

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func runnerStrPtr(value string) *string { return &value }

func runnerModelInfo(api schema.ModelAPI, variants []schema.ModelVariant) schema.ModelInfo {
	id := schema.ModelID("api-test-model")
	api.ID = &id
	return schema.ModelInfo{
		ID:           "test-model",
		ProviderID:   "test-provider",
		Name:         "Test model",
		API:          api,
		Capabilities: schema.ModelCapabilities{Tools: true, Input: []string{"text"}, Output: []string{"text"}},
		Request: schema.ModelRequest{
			Headers: map[string]string{"x-test": "header"},
			Body:    map[string]any{"apiKey": "secret", "custom_extension": map[string]any{"enabled": true}},
		},
		Variants: variants,
		Time:     schema.ModelTime{Released: 0},
		Cost:     []schema.ModelCost{},
		Status:   "active",
		Enabled:  true,
		Limit:    schema.ModelLimit{Context: 100, Output: 20},
	}
}

func runnerSession(model schema.ModelInfo, variant *schema.ModelVariantID) schema.SessionInfo {
	return schema.SessionInfo{Model: &schema.ModelRef{
		ID:         model.ID,
		ProviderID: model.ProviderID,
		Variant:    variant,
	}}
}

func TestFromCatalogModelOpenAI(t *testing.T) {
	resolved, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil), schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(resolved.ID) != "api-test-model" || string(resolved.Provider) != "test-provider" {
		t.Fatalf("model = %s/%s", resolved.Provider, resolved.ID)
	}
	if resolved.Route.ID != "openai-responses" {
		t.Fatalf("route = %s", resolved.Route.ID)
	}
	if resolved.Route.Endpoint.BaseURL != "https://openai.example/v1" {
		t.Fatalf("baseURL = %s", resolved.Route.Endpoint.BaseURL)
	}
	if resolved.Route.Defaults.Headers["x-test"] != "header" {
		t.Fatalf("headers = %+v", resolved.Route.Defaults.Headers)
	}
	if limits := resolved.Route.Defaults.Limits; limits == nil || limits.Context != 100 || limits.Output != 20 {
		t.Fatalf("limits = %+v", limits)
	}
	if body := map[string]any(resolved.Route.Defaults.Http.Body); !reflect.DeepEqual(body, map[string]any{
		"custom_extension": map[string]any{"enabled": true},
	}) {
		t.Fatalf("body = %+v", body)
	}
}

func TestTurnMediaSupportGatesProtocolModelAndPayload(t *testing.T) {
	info := runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-chat", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)
	info.Capabilities.Input = nil // protocol-only: exercise the wire set and payload
	model, err := FromCatalogModel(info, schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	policy := mediaPolicyForModel(model, core.AgentSelection{})
	if !policy.allows("user", "image/png", "inline", "data:image/png;base64,aGVsbG8=") {
		t.Fatal("a valid image should be accepted")
	}
	if policy.allows("user", "application/pdf", "inline", "data:application/pdf;base64,JVBERi0=") {
		t.Fatal("a document the protocol cannot carry should be rejected")
	}
	if policy.allows("user", "image/png", "inline", "notbase64!!!") {
		t.Fatal("a malformed payload should be rejected")
	}
	// openai-chat carries inline and url images, but not provider file refs.
	if got := policy.reason("user", "image/jpeg", "file", "file-api-123"); got != string(schema.AttachmentOmitUnsupportedSource) {
		t.Fatalf("file-source reason = %q, want unsupported_source", got)
	}
	if got := policy.reason("user", "image/jpeg", "url", "https://example.com/a.jpg"); got != "" {
		t.Fatalf("url should be allowed for openai-chat, got %q", got)
	}

	info.Capabilities.Input = []string{"text"} // model declares no image support
	textOnly, err := FromCatalogModel(info, schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mediaPolicyForModel(textOnly, core.AgentSelection{}).allows("user", "image/png", "inline", "data:image/png;base64,aGVsbG8=") {
		t.Fatal("a text-only model should reject an image")
	}

	a2a := core.AgentSelection{Info: &schema.AgentInfo{Backend: &schema.AgentBackend{Type: schema.AgentBackendA2A}}}
	if mediaPolicyForModel(model, a2a).reason != nil {
		t.Fatal("an A2A backend carries arbitrary file parts and should not be filtered")
	}
}

func TestFromCatalogModelCarriesCapabilities(t *testing.T) {
	resolved, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil), schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	caps := resolved.GetCapabilities()
	if caps == nil || !reflect.DeepEqual(caps.Input, []string{"text"}) {
		t.Fatalf("capabilities = %+v, want input [text]", caps)
	}
}

func TestFromCatalogModelAnthropic(t *testing.T) {
	resolved, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Protocol: "anthropic-messages", URL: runnerStrPtr("https://anthropic.example/v1"),
	}, nil), schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Route.ID != "anthropic-messages" || resolved.Route.Endpoint.BaseURL != "https://anthropic.example/v1" {
		t.Fatalf("route = %s %s", resolved.Route.ID, resolved.Route.Endpoint.BaseURL)
	}
}

func TestFromCatalogModelCompatibleAuth(t *testing.T) {
	info := runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-compatible-chat", URL: runnerStrPtr("https://compatible.example/v1"),
	}, nil)
	info.Request = schema.ModelRequest{Headers: map[string]string{}, Body: map[string]any{}}

	resolved, err := FromCatalogModel(info, schema.Credential{APIKey: "settings-secret"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	headers, err := resolved.Route.Auth.Apply(route.AuthInput{Headers: map[string]string{}})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if headers["authorization"] != "Bearer settings-secret" {
		t.Fatalf("headers = %+v", headers)
	}
	if body := map[string]any(resolved.Route.Defaults.Http.Body); !reflect.DeepEqual(body, map[string]any{}) {
		t.Fatalf("body = %+v", body)
	}
}

func TestResolveModelOverlaysVariant(t *testing.T) {
	high := schema.ModelVariantID("high")
	catalog := runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, []schema.ModelVariant{{
		ID:      "high",
		Headers: map[string]string{"x-variant": "high"},
		Body: map[string]any{
			"store": false, "service_tier": "priority", "temperature": 0.2,
			"reasoning": map[string]any{"effort": "high"},
		},
	}})

	resolved, err := ResolveModel(runnerSession(catalog, &high), catalog, schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Route.Defaults.Headers["x-test"] != "header" || resolved.Route.Defaults.Headers["x-variant"] != "high" {
		t.Fatalf("headers = %+v", resolved.Route.Defaults.Headers)
	}
	want := map[string]any{
		"custom_extension": map[string]any{"enabled": true},
		"store":            false,
		"service_tier":     "priority",
		"temperature":      0.2,
		"reasoning":        map[string]any{"effort": "high"},
	}
	if body := map[string]any(resolved.Route.Defaults.Http.Body); !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %+v want %+v", body, want)
	}
}

func TestResolveModelRejectsUnavailableVariant(t *testing.T) {
	unknown := schema.ModelVariantID("unknown")
	catalog := runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)

	_, err := ResolveModel(runnerSession(catalog, &unknown), catalog, schema.Credential{})
	var target *VariantUnavailableError
	if !errors.As(err, &target) {
		t.Fatalf("expected VariantUnavailableError, got %v", err)
	}
	if target.Error() != "Variant unavailable for test-provider/test-model: unknown" {
		t.Fatalf("message = %q", target.Error())
	}
}

func TestFromCatalogModelRejectsUnsupportedApi(t *testing.T) {
	_, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Protocol: "bogus", URL: runnerStrPtr("https://google.example/v1"),
	}, nil), schema.Credential{})
	var target *UnsupportedApiError
	if !errors.As(err, &target) {
		t.Fatalf("expected UnsupportedApiError, got %v", err)
	}
	if target.API != "bogus" || target.Error() != "Unsupported API for test-provider/test-model: bogus" {
		t.Fatalf("error = %+v", target)
	}
}

func TestFromCatalogModelUsesCredentialKey(t *testing.T) {
	credential := schema.Credential{APIKey: "secret"}
	info := runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)
	info.Request = schema.ModelRequest{Headers: map[string]string{}, Body: map[string]any{}}

	resolved, err := FromCatalogModel(info, credential)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	headers, err := resolved.Route.Auth.Apply(route.AuthInput{Headers: map[string]string{}})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if headers["authorization"] != "Bearer secret" {
		t.Fatalf("headers = %+v", headers)
	}
}

func TestFromCatalogModelDropsBodyAPIKey(t *testing.T) {
	credential := schema.Credential{APIKey: "stored-secret"}
	info := runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)
	info.Request = schema.ModelRequest{Headers: map[string]string{}, Body: map[string]any{"apiKey": "configured-secret"}}

	resolved, err := FromCatalogModel(info, credential)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	headers, err := resolved.Route.Auth.Apply(route.AuthInput{Headers: map[string]string{}})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if headers["authorization"] != "Bearer stored-secret" {
		t.Fatalf("headers = %+v", headers)
	}
	// A stray body apiKey is dropped rather than sent to the provider.
	if body := map[string]any(resolved.Route.Defaults.Http.Body); !reflect.DeepEqual(body, map[string]any{}) {
		t.Fatalf("body = %+v", body)
	}
}

func TestResolvedModelRouteDefaultsAndJSON(t *testing.T) {
	resolved, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Protocol: "openai-responses", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil), schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defaults := resolved.GetRouteDefaults()
	if defaults == nil || defaults.Limits == nil || defaults.Limits.Context != 100 || defaults.Limits.Output != 20 {
		t.Fatalf("route defaults = %+v", defaults)
	}
	data, err := json.Marshal(resolved)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"id":"api-test-model"`) || strings.Contains(string(data), "openai-responses") {
		t.Fatalf("json = %s", data)
	}
}

func TestResolveModelAnthropicVariant(t *testing.T) {
	high := schema.ModelVariantID("high")
	catalog := runnerModelInfo(schema.ModelAPI{
		Protocol: "anthropic-messages", URL: runnerStrPtr("https://anthropic.example/v1"),
	}, []schema.ModelVariant{{
		ID:   "high",
		Body: map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 12000}},
	}})
	resolved, err := ResolveModel(runnerSession(catalog, &high), catalog, schema.Credential{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := map[string]any{
		"custom_extension": map[string]any{"enabled": true},
		"thinking":         map[string]any{"type": "enabled", "budget_tokens": 12000},
	}
	if body := map[string]any(resolved.Route.Defaults.Http.Body); !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %+v", body)
	}
}

func TestFromCatalogModelRejectsRouteWithoutEndpoint(t *testing.T) {
	// Bedrock has no default endpoint (it is region-specific), so a model with no
	// URL must yield an error rather than panicking in Route.Model.
	model := runnerModelInfo(schema.ModelAPI{Protocol: "bedrock-converse"}, nil)
	if _, err := FromCatalogModel(model, schema.Credential{}); err == nil {
		t.Fatal("expected an error for a Bedrock model without an endpoint")
	}

	url := "https://bedrock-runtime.us-east-1.amazonaws.com"
	model = runnerModelInfo(schema.ModelAPI{Protocol: "bedrock-converse", URL: &url}, nil)
	if _, err := FromCatalogModel(model, schema.Credential{}); err != nil {
		t.Fatalf("bedrock with endpoint rejected: %v", err)
	}

	// Protocols with a built-in default endpoint need no URL.
	model = runnerModelInfo(schema.ModelAPI{Protocol: "openai-responses"}, nil)
	if _, err := FromCatalogModel(model, schema.Credential{APIKey: "k"}); err != nil {
		t.Fatalf("openai-responses rejected: %v", err)
	}
}
