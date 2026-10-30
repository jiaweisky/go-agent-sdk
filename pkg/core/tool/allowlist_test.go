package tool_test

import (
	"context"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core/tool"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

func TestRestrict(t *testing.T) {
	called := ""
	mat := &tool.Materialization{
		Definitions: []llmschema.ToolDefinition{{Name: "bash"}, {Name: "read"}, {Name: "grep"}},
		Settle: func(context.Context, tool.ExecuteInput) (*tool.Settlement, error) {
			called = "inner"
			return &tool.Settlement{}, nil
		},
	}

	filtered := tool.Restrict(mat, []string{"bash", "read"})
	if len(filtered.Definitions) != 2 {
		t.Fatalf("definitions = %+v", filtered.Definitions)
	}
	for _, def := range filtered.Definitions {
		if def.Name != "bash" && def.Name != "read" {
			t.Fatalf("unexpected tool in definitions: %s", def.Name)
		}
	}

	// A listed tool reaches the inner settle.
	if _, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "read"}}); err != nil {
		t.Fatalf("settle read: %v", err)
	}
	if called != "inner" {
		t.Fatalf("inner settle not called (called=%q)", called)
	}

	// A filtered-out tool is rejected as unknown, like the registry does.
	called = ""
	settlement, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "grep"}})
	if err != nil {
		t.Fatalf("settle grep: %v", err)
	}
	if called != "" || settlement.Result.Type != "error" || settlement.Result.Value != "Unknown tool: grep" {
		t.Fatalf("grep settlement = %+v (called=%q)", settlement, called)
	}
}

func TestRestrictNilVersusEmptyList(t *testing.T) {
	mat := &tool.Materialization{
		Definitions: []llmschema.ToolDefinition{{Name: "bash"}},
		Settle: func(context.Context, tool.ExecuteInput) (*tool.Settlement, error) {
			t.Error("inner settle must not be reached")
			return &tool.Settlement{}, nil
		},
	}

	// nil (the unset default) keeps every tool.
	if got := tool.Restrict(mat, nil); got != mat {
		t.Fatal("nil list must return the materialization unchanged")
	}

	// An explicit empty list rejects every tool: nothing is listed, and any
	// call is answered with the unknown-tool error.
	filtered := tool.Restrict(mat, []string{})
	if len(filtered.Definitions) != 0 {
		t.Fatalf("definitions = %+v", filtered.Definitions)
	}
	settlement, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "bash"}})
	if err != nil {
		t.Fatalf("settle bash: %v", err)
	}
	if settlement.Result.Type != "error" || settlement.Result.Value != "Unknown tool: bash" {
		t.Fatalf("bash settlement = %+v", settlement)
	}
}

func TestExclude(t *testing.T) {
	called := ""
	mat := &tool.Materialization{
		Definitions: []llmschema.ToolDefinition{{Name: "bash"}, {Name: "read"}, {Name: "grep"}},
		Settle: func(context.Context, tool.ExecuteInput) (*tool.Settlement, error) {
			called = "inner"
			return &tool.Settlement{}, nil
		},
	}

	if got := tool.Exclude(mat, nil); got != mat {
		t.Fatal("nil list must return the materialization unchanged")
	}
	if got := tool.Exclude(mat, []string{}); got != mat {
		t.Fatal("empty list must return the materialization unchanged")
	}

	filtered := tool.Exclude(mat, []string{"bash"})
	if len(filtered.Definitions) != 2 {
		t.Fatalf("definitions = %+v", filtered.Definitions)
	}
	for _, def := range filtered.Definitions {
		if def.Name == "bash" {
			t.Fatal("bash must be excluded")
		}
	}

	if _, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "read"}}); err != nil {
		t.Fatalf("settle read: %v", err)
	}
	if called != "inner" {
		t.Fatalf("inner settle not called (called=%q)", called)
	}

	called = ""
	settlement, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "bash"}})
	if err != nil {
		t.Fatalf("settle bash: %v", err)
	}
	if called != "" || settlement.Result.Type != "error" || settlement.Result.Value != "Unknown tool: bash" {
		t.Fatalf("bash settlement = %+v (called=%q)", settlement, called)
	}
}

// TestRestrictThenExcludeOverlap applies both filters to the same name: the
// blacklist runs after the allowlist, so exclusion wins.
func TestRestrictThenExcludeOverlap(t *testing.T) {
	called := ""
	mat := &tool.Materialization{
		Definitions: []llmschema.ToolDefinition{{Name: "bash"}, {Name: "read"}, {Name: "grep"}},
		Settle: func(context.Context, tool.ExecuteInput) (*tool.Settlement, error) {
			called = "inner"
			return &tool.Settlement{}, nil
		},
	}

	filtered := tool.Exclude(tool.Restrict(mat, []string{"bash", "read"}), []string{"bash"})
	if len(filtered.Definitions) != 1 || filtered.Definitions[0].Name != "read" {
		t.Fatalf("definitions = %+v", filtered.Definitions)
	}

	if _, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "read"}}); err != nil {
		t.Fatalf("settle read: %v", err)
	}
	if called != "inner" {
		t.Fatalf("inner settle not called (called=%q)", called)
	}

	called = ""
	settlement, err := filtered.Settle(context.Background(), tool.ExecuteInput{Call: llmschema.ToolCallPart{Name: "bash"}})
	if err != nil {
		t.Fatalf("settle bash: %v", err)
	}
	if called != "" || settlement.Result.Type != "error" || settlement.Result.Value != "Unknown tool: bash" {
		t.Fatalf("bash settlement = %+v (called=%q)", settlement, called)
	}
}
