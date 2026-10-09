package protocols

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestGeminiLowerToolResultJoinsTextWithoutTrailingNewline(t *testing.T) {
	request := &schema.LLMRequest{
		Messages: []schema.Message{
			{Role: schema.RoleAssistant, Content: []schema.ContentPart{
				schema.ToolCallPart{Type: "tool-call", ID: "call_image", Name: "read", Input: map[string]any{"path": "pixel.png"}},
			}},
			{Role: schema.RoleTool, Content: []schema.ContentPart{
				schema.ToolResultPart{
					Type: "tool-result",
					ID:   "call_image",
					Name: "read",
					Result: schema.ToolResultValue{Type: "content", Value: []schema.ToolContent{
						schema.ToolTextContent{Type: "text", Text: "Image read successfully"},
						schema.ToolTextContent{Type: "text", Text: "second line"},
						schema.ToolFileContent{Type: "file", URI: "data:image/png;base64,AAECAw==", Mime: "image/png"},
					}},
				},
			}},
		},
	}

	contents, err := geminiLowerMessages(request)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	raw, err := json.Marshal(contents)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body []map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("contents = %s", raw)
	}
	parts, _ := body[1]["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %v", parts)
	}
	response, _ := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	inner, _ := response["response"].(map[string]any)
	if inner["content"] != "Image read successfully\nsecond line" {
		t.Fatalf("content = %q", inner["content"])
	}
	inline, _ := parts[1].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "image/png" || inline["data"] != "AAECAw==" {
		t.Fatalf("inlineData = %v", parts[1])
	}
	if strings.Contains(string(raw), `"content":"AAECAw=="`) {
		t.Fatalf("base64 leaked into the text content: %s", raw)
	}
}

func TestGeminiLowerToolResultEmptyTextContent(t *testing.T) {
	request := &schema.LLMRequest{
		Messages: []schema.Message{
			{Role: schema.RoleTool, Content: []schema.ContentPart{
				schema.ToolResultPart{
					Type: "tool-result",
					ID:   "call_1",
					Name: "read",
					Result: schema.ToolResultValue{Type: "content", Value: []schema.ToolContent{
						schema.ToolFileContent{Type: "file", URI: "data:image/jpeg;base64,/9j/", Mime: "image/jpeg"},
					}},
				},
			}},
		},
	}

	contents, err := geminiLowerMessages(request)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	raw, err := json.Marshal(contents)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body []map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	parts, _ := body[0]["parts"].([]any)
	response, _ := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	inner, _ := response["response"].(map[string]any)
	if inner["content"] != "" {
		t.Fatalf("content = %q, want empty", inner["content"])
	}
}
