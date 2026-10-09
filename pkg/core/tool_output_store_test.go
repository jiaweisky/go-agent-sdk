package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
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

func TestToolOutputStoreCleanupRemovesExpiredFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewToolOutputStore(Global{Data: dir}, ToolOutputSettings{})
	managed := filepath.Join(dir, ToolOutputStoreManagedDirectory)
	if err := EnsureDir(managed); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	expired := filepath.Join(managed, "tool_expired")
	fresh := filepath.Join(managed, "tool_fresh")
	other := filepath.Join(managed, "notes.txt")
	for _, file := range []string{expired, fresh, other} {
		if err := WriteFileBytes(file, []byte("x"), "w"); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	past := time.Now().Add(-ToolOutputStoreRetention - time.Hour)
	if err := os.Chtimes(expired, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("expired file was not removed: %v", err)
	}
	for _, file := range []string{fresh, other} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("%s was removed: %v", file, err)
		}
	}
}

func TestToolOutputStoreCleanupWithoutDirectory(t *testing.T) {
	store := NewToolOutputStore(Global{Data: t.TempDir()}, ToolOutputSettings{})
	if err := store.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
