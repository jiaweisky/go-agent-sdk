package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
)

// TestEmitLinesTool guards the large-output tool used to trigger truncation.
func TestEmitLinesTool(t *testing.T) {
	tool, ok := runtimeTool("emit_lines")
	if !ok {
		t.Fatal("emit_lines missing")
	}
	out, err := tool.Handler(context.Background(), map[string]any{"count": float64(3)}, agent.ToolContext{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out != "line 1\nline 2\nline 3" {
		t.Fatalf("out = %q", out)
	}
}

// TestToolOutputCommand guards the report and the cleanup count: the SDK reports
// no count, so the example counts the spill files itself.
func TestToolOutputCommand(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		dir := c.toolOutputDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"tool_1", "tool_2"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		handleCommand(c, "/tool-output")
		c.console.write("--cleanup--\n")
		handleCommand(c, "/tool-output cleanup")
	})
	for _, want := range []string{"Tool output:", "2 file(s)", "--cleanup--", "scanned 2, removed 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}
