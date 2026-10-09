package protocols

import (
	"encoding/json"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func moonshotTupleSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"coords": map[string]any{
				"prefixItems": []any{map[string]any{"type": "string"}},
			},
		},
	}
}

func TestProjectToolSchema(t *testing.T) {
	inputSchema := moonshotTupleSchema()

	raw, _ := ProjectToolSchema(schema.Model{}, inputSchema).(map[string]any)
	coords, _ := raw["properties"].(map[string]any)["coords"].(map[string]any)
	if _, ok := coords["prefixItems"]; !ok {
		t.Fatalf("without compatibility the schema must pass through: %#v", coords)
	}

	projected, _ := ProjectToolSchema(
		schema.Model{Compatibility: schema.NewModelCompatibility(schema.ModelToolSchemaMoonshot)},
		inputSchema,
	).(map[string]any)
	coords, _ = projected["properties"].(map[string]any)["coords"].(map[string]any)
	if _, ok := coords["prefixItems"]; ok {
		t.Fatalf("projected schema kept prefixItems: %#v", coords)
	}
	if items, ok := coords["items"].(map[string]any); !ok || items["type"] != "string" {
		t.Fatalf("projected items = %#v", coords["items"])
	}
}

func TestOpenAIChatProjectsToolSchemaForCompatibleModel(t *testing.T) {
	r := OpenAIChatHTTPRoute.With(route.RoutePatch{Provider: "test"})
	request := &schema.LLMRequest{
		Model: mustBindModel(t, r, route.RouteModelInput{
			ID:            "moonshot-model",
			Compatibility: schema.NewModelCompatibility(schema.ModelToolSchemaMoonshot),
		}),
		Tools: []schema.ToolDefinition{{Name: "lookup", Description: "Look up", InputSchema: moonshotTupleSchema()}},
	}

	body, err := r.Body.From(request)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsJSONKey(encoded, "prefixItems") {
		t.Fatalf("tool schema was not projected: %s", encoded)
	}
	if !containsJSONKey(encoded, "items") {
		t.Fatalf("projected schema is missing items: %s", encoded)
	}
}

func containsJSONKey(encoded []byte, key string) bool {
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return false
	}
	return jsonHasKey(decoded, key)
}

func jsonHasKey(value any, key string) bool {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if k == key || jsonHasKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if jsonHasKey(child, key) {
				return true
			}
		}
	}
	return false
}

func TestToolSchemaIsSentForEveryProtocol(t *testing.T) {
	inputSchema := schema.JsonSchema{
		"type":       "object",
		"properties": map[string]any{"query": map[string]any{"type": "string"}},
	}
	cases := []struct {
		name  string
		route route.Route
	}{
		{"openai-chat", OpenAIChatHTTPRoute.With(route.RoutePatch{Provider: "test"})},
		{"openai-compatible-chat", OpenAICompatibleChatHTTPRoute.With(route.RoutePatch{
			Provider: "test",
			Endpoint: &route.EndpointPatch{BaseURL: new("https://compatible.test")},
		})},
		{"openai-responses", OpenAIResponsesHTTPRoute.With(route.RoutePatch{Provider: "test"})},
		{"anthropic", AnthropicMessagesHTTPRoute.With(route.RoutePatch{Provider: "test"})},
		{"gemini", GeminiHTTPRoute.With(route.RoutePatch{Provider: "test"})},
		{"bedrock", BedrockConverseRoute(BedrockConverseProtocol).With(route.RoutePatch{
			Provider: "test",
			Endpoint: &route.EndpointPatch{BaseURL: new("https://bedrock.test")},
		})},
	}
	for _, tc := range cases {
		r := tc.route
		body, err := r.Body.From(&schema.LLMRequest{
			Model: mustBindModel(t, r, route.RouteModelInput{ID: "test-model"}),
			Tools: []schema.ToolDefinition{{Name: "lookup", Description: "Look up", InputSchema: inputSchema}},
		})
		if err != nil {
			t.Fatalf("%s: body: %v", tc.name, err)
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		if !containsJSONKey(encoded, "query") {
			t.Fatalf("%s: tool schema missing from the request body: %s", tc.name, encoded)
		}
	}
}

func TestOpenAIReasoningEffortValidation(t *testing.T) {
	cases := []struct {
		name  string
		route route.Route
	}{
		{"openai-chat", OpenAIChatHTTPRoute},
		{"openai-responses", OpenAIResponsesHTTPRoute},
	}
	for _, tc := range cases {
		r := tc.route.With(route.RoutePatch{Provider: "test"})
		model := mustBindModel(t, r, route.RouteModelInput{ID: "test-model"})

		if _, err := r.Body.From(&schema.LLMRequest{
			Model:           model,
			ProviderOptions: schema.ProviderOptions{"openai": {"reasoningEffort": "max"}},
		}); err == nil {
			t.Fatalf("%s: want an error for reasoningEffort=max", tc.name)
		}

		body, err := r.Body.From(&schema.LLMRequest{
			Model:           model,
			ProviderOptions: schema.ProviderOptions{"openai": {"reasoningEffort": "bogus"}},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		if containsJSONKey(encoded, "reasoning_effort") || containsJSONKey(encoded, "reasoning") {
			t.Fatalf("%s: invalid effort leaked into the body: %s", tc.name, encoded)
		}
	}
}

func TestOpenAIResponsesRejectsUnsupportedContent(t *testing.T) {
	r := OpenAIResponsesHTTPRoute.With(route.RoutePatch{Provider: "test"})
	model := mustBindModel(t, r, route.RouteModelInput{ID: "test-model"})

	cases := []struct {
		name    string
		role    schema.MessageRole
		content []schema.ContentPart
	}{
		{"user tool-result", schema.RoleUser, []schema.ContentPart{
			schema.ToolResultPart{Type: "tool-result", ID: "call_1", Name: "lookup"},
		}},
		{"tool text", schema.RoleTool, []schema.ContentPart{
			schema.TextPart{Type: "text", Text: "nope"},
		}},
		{"assistant user-text", schema.RoleAssistant, []schema.ContentPart{
			schema.MediaPart{Type: "media", MediaType: "image/png", Data: "data:image/png;base64,AAECAw=="},
		}},
	}
	for _, tc := range cases {
		_, err := r.Body.From(&schema.LLMRequest{
			Model:    model,
			Messages: []schema.Message{{Role: tc.role, Content: tc.content}},
		})
		if err == nil {
			t.Fatalf("%s: want an error", tc.name)
		}
	}
}
