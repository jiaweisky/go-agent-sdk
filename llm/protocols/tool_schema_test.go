package protocols

import (
	"encoding/json"
	"testing"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
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
		Model: r.Model(route.RouteModelInput{
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
		{"openai-chat", OpenAIChatHTTPRoute},
		{"openai-compatible-chat", OpenAICompatibleChatHTTPRoute},
		{"openai-responses", OpenAIResponsesHTTPRoute},
		{"anthropic", AnthropicMessagesHTTPRoute},
		{"gemini", GeminiHTTPRoute},
		{"bedrock", BedrockConverseRoute(BedrockConverseProtocol)},
	}
	for _, tc := range cases {
		r := tc.route.With(route.RoutePatch{Provider: "test"})
		body, err := r.Body.From(&schema.LLMRequest{
			Model: r.Model(route.RouteModelInput{ID: "test-model"}),
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
