package agent

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestMapStepEndedFiles guards that the high-level step event carries the
// changed files the low-level payload has.
func TestMapStepEndedFiles(t *testing.T) {
	ended, ok := mapEvent(schema.EventPayload{
		Type: schema.SessionEventStepEnded.Type,
		Data: map[string]any{
			"sessionID": "ses_1", "assistantMessageID": "m1", "finish": "stop",
			"files": []schema.RelativePath{"a.go", "b.go"},
		},
	})
	if !ok {
		t.Fatal("step.ended was not mapped")
	}
	step := ended.(StepEnded)
	if len(step.Files) != 2 || step.Files[0] != "a.go" {
		t.Fatalf("StepEnded.Files = %v", step.Files)
	}
}

// TestMapToolSuccessStructured guards that the tool's structured output (its
// file diffs) reaches ToolSuccess.
func TestMapToolSuccessStructured(t *testing.T) {
	structured := map[string]any{"files": []any{map[string]any{"file": "a.go", "patch": "@@ -1 +1 @@"}}}
	event, ok := mapEvent(schema.EventPayload{
		Type: schema.SessionEventToolSuccess.Type,
		Data: map[string]any{"sessionID": "ses_1", "callID": "c1", "structured": structured},
	})
	if !ok {
		t.Fatal("tool.success was not mapped")
	}
	if event.(ToolSuccess).Structured == nil {
		t.Fatal("ToolSuccess.Structured was dropped")
	}
}

// TestClientCapture guards that two captures of an unchanged tree are equal
// (content-addressed), which is the basis for turn diffs.
func TestClientCapture(t *testing.T) {
	client := newTestClient(t)
	before, ok := client.Capture()
	if !ok {
		t.Skip("snapshots unavailable in this environment")
	}
	after, ok := client.Capture()
	if !ok || before != after {
		t.Fatalf("captures = %q, %q; want equal ids", before, after)
	}
}

// TestClientDiffNoSnapshot guards that Diff is a no-op without snapshot ids.
func TestClientDiffNoSnapshot(t *testing.T) {
	client := newTestClient(t)
	if diffs, err := client.Diff("", ""); err != nil || diffs != nil {
		t.Fatalf("Diff = %v, %v; want nil, nil", diffs, err)
	}
}
