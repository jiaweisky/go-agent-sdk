package protocols

import (
	"encoding/json"
	"testing"

	"github.com/user/go-agent-sdk/llm/schema"
)

func TestOpenAIChatLowerAssistantJoinsTextWithNewline(t *testing.T) {
	got, err := openaiChatLowerAssistantMessage(schema.Message{
		Role: schema.RoleAssistant,
		Content: []schema.ContentPart{
			schema.TextPart{Type: "text", Text: "line one"},
			schema.TextPart{Type: "text", Text: "line two"},
		},
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if got.Content != "line one\nline two" {
		t.Fatalf("content = %#v", got.Content)
	}
}

func TestOpenAIChatLowerAssistantReasoning(t *testing.T) {
	got, err := openaiChatLowerAssistantMessage(schema.Message{
		Role: schema.RoleAssistant,
		Content: []schema.ContentPart{
			schema.ReasoningPart{Type: "reasoning", Text: "thinking"},
			schema.TextPart{Type: "text", Text: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"role":"assistant","content":"Hello","reasoning_content":"thinking"}` {
		t.Fatalf("message = %s", raw)
	}
}

func TestOpenAIChatLowerAssistantReasoningOnly(t *testing.T) {
	got, err := openaiChatLowerAssistantMessage(schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentPart{schema.ReasoningPart{Type: "reasoning", Text: "hidden"}},
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"role":"assistant","content":null,"reasoning_content":"hidden"}` {
		t.Fatalf("message = %s", raw)
	}
}

func TestOpenAIChatLowerAssistantUsesCompatibleNativeReasoning(t *testing.T) {
	got, err := openaiChatLowerAssistantMessage(schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentPart{schema.TextPart{Type: "text", Text: "Hello"}},
		Native:  map[string]any{"openaiCompatible": map[string]any{"reasoning_content": "replayed"}},
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if got.ReasoningContent != "replayed" {
		t.Fatalf("reasoning_content = %q", got.ReasoningContent)
	}
}
