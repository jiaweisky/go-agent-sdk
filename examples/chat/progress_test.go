package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core/tool"
)

// TestCountToToolEmitsProgress guards that count_to reports each step through
// ToolContext.Progress.
func TestCountToToolEmitsProgress(t *testing.T) {
	handler, ok := runtimeTool("count_to")
	if !ok {
		t.Fatal("runtime tool count_to missing")
	}
	var steps []int
	if _, err := handler.Handler(context.Background(), map[string]any{"n": float64(2)}, agent.ToolContext{
		Progress: func(structured map[string]any, _ []tool.Content) error {
			steps = append(steps, structured["current"].(int))
			return nil
		},
	}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(steps) != 2 || steps[0] != 1 || steps[1] != 2 {
		t.Fatalf("steps = %v, want [1 2]", steps)
	}
}

// TestClampSteps guards count_to's bounds and default.
func TestClampSteps(t *testing.T) {
	for in, want := range map[int]int{-1: 5, 0: 5, 1: 1, 7: 7, 10: 10, 99: 10} {
		if got := clampSteps(in); got != want {
			t.Errorf("clampSteps(%d) = %d, want %d", in, got, want)
		}
	}
}

// TestFormatProgress guards the stable key ordering of a progress line.
func TestFormatProgress(t *testing.T) {
	got := formatProgress(map[string]any{"total": 3, "current": 1})
	if got != "current=1 total=3" {
		t.Fatalf("formatProgress = %q, want %q", got, "current=1 total=3")
	}
}

// TestHandleProgressRendersUserTool guards that non-task progress is rendered.
func TestHandleProgressRendersUserTool(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.handleProgress(agent.ToolProgress{
			CallID: "call_1", Structured: map[string]any{"current": 1, "total": 3},
		})
	})
	if !strings.Contains(out, "[progress] current=1 total=3") {
		t.Fatalf("out = %q, want the progress line", out)
	}
}
