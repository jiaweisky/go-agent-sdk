package runner

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/schema"
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
		Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil), nil)
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

func TestFromCatalogModelAnthropic(t *testing.T) {
	resolved, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Type: "aisdk", Package: "@ai-sdk/anthropic", URL: runnerStrPtr("https://anthropic.example/v1"),
	}, nil), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Route.ID != "anthropic-messages" || resolved.Route.Endpoint.BaseURL != "https://anthropic.example/v1" {
		t.Fatalf("route = %s %s", resolved.Route.ID, resolved.Route.Endpoint.BaseURL)
	}
}

func TestFromCatalogModelCompatibleSettingsAuth(t *testing.T) {
	info := runnerModelInfo(schema.ModelAPI{
		Type: "aisdk", Package: "@ai-sdk/openai-compatible", URL: runnerStrPtr("https://compatible.example/v1"),
		Settings: map[string]any{"apiKey": "settings-secret", "compatibility": "strict"},
	}, nil)
	info.Request = schema.ModelRequest{Headers: map[string]string{}, Body: map[string]any{}}

	resolved, err := FromCatalogModel(info, nil)
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
		Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1"),
	}, []schema.ModelVariant{{
		ID:      "high",
		Headers: map[string]string{"x-variant": "high"},
		Body: map[string]any{
			"store": false, "service_tier": "priority", "temperature": 0.2,
			"reasoning": map[string]any{"effort": "high"},
		},
	}})

	resolved, err := ResolveModel(runnerSession(catalog, &high), catalog, nil)
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
		Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)

	_, err := ResolveModel(runnerSession(catalog, &unknown), catalog, nil)
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
		Type: "aisdk", Package: "@ai-sdk/google", URL: runnerStrPtr("https://google.example/v1"),
	}, nil), nil)
	var target *UnsupportedApiError
	if !errors.As(err, &target) {
		t.Fatalf("expected UnsupportedApiError, got %v", err)
	}
	if target.API != "aisdk:@ai-sdk/google" || target.Error() != "Unsupported API for test-provider/test-model: aisdk:@ai-sdk/google" {
		t.Fatalf("error = %+v", target)
	}
}

func TestSupported(t *testing.T) {
	openai := runnerModelInfo(schema.ModelAPI{Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1")}, nil)
	google := runnerModelInfo(schema.ModelAPI{Type: "aisdk", Package: "@ai-sdk/google", URL: runnerStrPtr("https://google.example/v1")}, nil)
	native := runnerModelInfo(schema.ModelAPI{Type: "native", Settings: map[string]any{}}, nil)
	if !Supported(openai) || Supported(google) || Supported(native) {
		t.Fatalf("supported = %v %v %v", Supported(openai), Supported(google), Supported(native))
	}
}

func TestFromCatalogModelUsesCredentialKey(t *testing.T) {
	credential := "secret"
	info := runnerModelInfo(schema.ModelAPI{
		Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)
	info.Request = schema.ModelRequest{Headers: map[string]string{}, Body: map[string]any{}}

	resolved, err := FromCatalogModel(info, &credential)
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

func TestFromCatalogModelCredentialOverridesConfiguredKey(t *testing.T) {
	credential := "stored-secret"
	info := runnerModelInfo(schema.ModelAPI{
		Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil)
	info.Request = schema.ModelRequest{Headers: map[string]string{}, Body: map[string]any{"apiKey": "configured-secret"}}

	resolved, err := FromCatalogModel(info, &credential)
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
	if body := map[string]any(resolved.Route.Defaults.Http.Body); !reflect.DeepEqual(body, map[string]any{}) {
		t.Fatalf("body = %+v", body)
	}
}

func TestResolvedModelRouteDefaultsAndJSON(t *testing.T) {
	resolved, err := FromCatalogModel(runnerModelInfo(schema.ModelAPI{
		Type: "aisdk", Package: "@ai-sdk/openai", URL: runnerStrPtr("https://openai.example/v1"),
	}, nil), nil)
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
		Type: "aisdk", Package: "@ai-sdk/anthropic", URL: runnerStrPtr("https://anthropic.example/v1"),
	}, []schema.ModelVariant{{
		ID:   "high",
		Body: map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 12000}},
	}})
	resolved, err := ResolveModel(runnerSession(catalog, &high), catalog, nil)
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
