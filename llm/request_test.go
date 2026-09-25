package llm

import (
	"testing"

	"github.com/user/go-agent-sdk/llm/schema"
)

func TestUpdateRequestKeepsUnsetFields(t *testing.T) {
	base := NewRequest(RequestInput{
		Model:      schema.Model{},
		Generation: &schema.GenerationOptions{},
		Http:       &schema.HttpOptions{},
	})
	updated := UpdateRequest(base, RequestInput{Prompt: "hello"})
	if updated.Model == nil {
		t.Fatal("UpdateRequest cleared Model")
	}
	if updated.Generation == nil {
		t.Fatal("UpdateRequest cleared Generation")
	}
	if updated.Http == nil {
		t.Fatal("UpdateRequest cleared Http")
	}
	if len(updated.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(updated.Messages))
	}
}

func TestMarkMessagesCachesTail(t *testing.T) {
	messages := []schema.Message{
		{Role: schema.RoleUser, Content: []schema.ContentPart{schema.TextPart{Type: "text", Text: "one"}}},
		{Role: schema.RoleAssistant, Content: []schema.ContentPart{schema.TextPart{Type: "text", Text: "two"}}},
	}
	tail := 1
	got := markMessages(messages, schema.CachePolicyMessages{Tail: &tail}, schema.NewCacheHint("ephemeral", nil))
	if got[1].Content[0].(schema.TextPart).Cache == nil {
		t.Fatal("tail message was not cached")
	}
	if got[0].Content[0].(schema.TextPart).Cache != nil {
		t.Fatal("non-tail message was cached")
	}
}
