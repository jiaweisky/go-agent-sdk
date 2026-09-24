package core

import (
	"strings"
	"testing"

	llmschema "github.com/user/go-agent-sdk/llm/schema"
)

func TestToolOutputStoreBoundKeepsSmallOutput(t *testing.T) {
	store := NewToolOutputStore(Global{Data: t.TempDir()}, ToolOutputSettings{})
	output := llmschema.ToolOutput{
		Structured: map[string]any{},
		Content:    []llmschema.ToolContent{llmschema.ToolTextContent{Type: "text", Text: "small"}},
	}
	result, err := store.Bound(ToolOutputStoreBoundInput{SessionID: "ses_1", ToolCallID: "call_1", Output: output})
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if len(result.OutputPaths) != 0 {
		t.Fatalf("paths = %v", result.OutputPaths)
	}
	if len(result.Output.Content) != 1 {
		t.Fatalf("content = %+v", result.Output.Content)
	}
}

func TestToolOutputStoreBoundTruncatesAndWrites(t *testing.T) {
	store := NewToolOutputStore(Global{Data: t.TempDir()}, ToolOutputSettings{})
	long := strings.Repeat("line\n", ToolOutputStoreMaxLines+100)
	output := llmschema.ToolOutput{
		Structured: map[string]any{"kept": true},
		Content: []llmschema.ToolContent{
			llmschema.ToolTextContent{Type: "text", Text: long},
		},
	}
	result, err := store.Bound(ToolOutputStoreBoundInput{SessionID: "ses_1", ToolCallID: "call_1", Output: output})
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if len(result.OutputPaths) != 1 || !strings.Contains(result.OutputPaths[0], "tool_") {
		t.Fatalf("paths = %v", result.OutputPaths)
	}
	text, ok := result.Output.Content[0].(llmschema.ToolTextContent)
	if !ok || !strings.Contains(text.Text, "output truncated; full content saved to") {
		t.Fatalf("content[0] = %+v", result.Output.Content[0])
	}
	if result.Output.Structured == nil {
		t.Fatalf("structured dropped")
	}
}

func TestToolOutputStoreBoundStructuredWhenNoContent(t *testing.T) {
	store := NewToolOutputStore(Global{Data: t.TempDir()}, ToolOutputSettings{})
	output := llmschema.ToolOutput{Structured: map[string]any{"a": 1}, Content: []llmschema.ToolContent{}}
	result, err := store.Bound(ToolOutputStoreBoundInput{SessionID: "ses_1", ToolCallID: "call_1", Output: output})
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if len(result.OutputPaths) != 0 {
		t.Fatalf("paths = %v", result.OutputPaths)
	}
}
