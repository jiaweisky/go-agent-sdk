package protocols

import (
	"encoding/json"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func bodySchemaRoutes() []struct {
	name  string
	route route.Route
} {
	return []struct {
		name  string
		route route.Route
	}{
		{"openai-chat", OpenAIChatHTTPRoute},
		{"openai-compatible-chat", OpenAICompatibleChatHTTPRoute},
		{"deepseek-chat", DeepSeekChatHTTPRoute},
		{"openai-responses", OpenAIResponsesHTTPRoute},
		{"anthropic", AnthropicMessagesHTTPRoute},
		{"gemini", GeminiHTTPRoute},
		{"bedrock", BedrockConverseRoute(BedrockConverseProtocol)},
	}
}

func validateLoweredBody(t *testing.T, name string, r route.Route, request *schema.LLMRequest) {
	t.Helper()
	if r.Body.Validator == nil {
		t.Fatalf("%s: no body validator", name)
	}
	body, err := r.Body.From(request)
	if err != nil {
		t.Fatalf("%s: body: %v", name, err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("%s: unmarshal: %v", name, err)
	}
	if err := r.Body.Validator.Validate(document); err != nil {
		t.Fatalf("%s: body failed validation: %v\n%s", name, err, encoded)
	}
}

func TestBodySchemasAcceptMinimalRequest(t *testing.T) {
	request := &schema.LLMRequest{Model: schema.Model{ID: "test-model", Provider: "test"}}
	for _, tc := range bodySchemaRoutes() {
		validateLoweredBody(t, tc.name, tc.route, request)
	}
}

func TestBodySchemasAcceptLoweredBodies(t *testing.T) {
	maxTokens := 64
	temperature := 0.2
	request := &schema.LLMRequest{
		Model:  schema.Model{ID: "test-model", Provider: "test"},
		System: []schema.SystemPart{{Type: "text", Text: "be brief"}},
		Messages: []schema.Message{
			{Role: schema.RoleUser, Content: []schema.ContentPart{
				schema.TextPart{Type: "text", Text: "hello"},
			}},
			{Role: schema.RoleAssistant, Content: []schema.ContentPart{
				schema.TextPart{Type: "text", Text: "hi"},
				schema.ToolCallPart{Type: "tool-call", ID: "call_1", Name: "lookup", Input: map[string]any{"q": "x"}},
			}},
			{Role: schema.RoleTool, Content: []schema.ContentPart{
				schema.ToolResultPart{
					Type: "tool-result",
					ID:   "call_1",
					Name: "lookup",
					Result: schema.ToolResultValue{Type: "content", Value: []schema.ToolContent{
						schema.ToolTextContent{Type: "text", Text: "sunny"},
					}},
				},
			}},
		},
		Tools:      []schema.ToolDefinition{{Name: "lookup", Description: "Look up", InputSchema: schema.JsonSchema{"type": "object"}}},
		Generation: &schema.GenerationOptions{MaxTokens: &maxTokens, Temperature: &temperature},
	}
	for _, tc := range bodySchemaRoutes() {
		validateLoweredBody(t, tc.name, tc.route, request)
	}
}

// TestBodySchemasAcceptURLAndFileMedia guards the "three touch-points" rule: a
// protocol that declares a source must lower it to a body its schema accepts.
func TestBodySchemasAcceptURLAndFileMedia(t *testing.T) {
	parts := []schema.MediaPart{
		{Type: "media", MediaType: "image/png", Source: schema.MediaSourceURL, Data: "https://example.com/a.png"},
		{Type: "media", MediaType: "image/png", Source: schema.MediaSourceFile, Data: "file-id-123"},
	}
	for _, tc := range bodySchemaRoutes() {
		entry, ok := LookupProtocol(schema.ProtocolID(tc.route.ID))
		if !ok {
			t.Fatalf("%s: not registered", tc.name)
		}
		for _, part := range parts {
			if !entry.Media.AllowsSource(part.Source) {
				continue
			}
			request := &schema.LLMRequest{
				Model:    schema.Model{ID: "test-model", Provider: "test"},
				Messages: []schema.Message{{Role: schema.RoleUser, Content: []schema.ContentPart{part}}},
			}
			validateLoweredBody(t, tc.name+"/"+string(part.Source), tc.route, request)
		}
	}
}
