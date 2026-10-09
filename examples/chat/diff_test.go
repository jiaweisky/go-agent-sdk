package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// TestSwitchSessionResetsSnapshot guards that switching sessions clears the
// turn's snapshot ids, so /diff cannot show the previous session's changes.
func TestSwitchSessionResetsSnapshot(t *testing.T) {
	runCommand(t, nil, func(c *chat) {
		c.turnStartSnapshot = "a"
		c.lastSnapshot = "b"
		session, err := c.client.CreateSession(context.Background(), &agent.SessionConfig{Agent: "build"})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		c.switchSession(session)
		if c.turnStartSnapshot != "" || c.lastSnapshot != "" {
			t.Fatalf("snapshots not reset: %q %q", c.turnStartSnapshot, c.lastSnapshot)
		}
	})
}

// TestToolDiffFiles guards extracting per-file diffs from a tool's structured
// output and ignoring other shapes.
func TestToolDiffFiles(t *testing.T) {
	structured := map[string]any{"files": []any{
		map[string]any{"file": "a.go", "patch": "@@ -1 +1 @@\n-old\n+new", "additions": float64(1), "deletions": float64(1)},
	}}
	files := toolDiffFiles(structured)
	if len(files) != 1 || files[0].Path != "a.go" || files[0].Additions != 1 || !strings.Contains(files[0].Patch, "+new") {
		t.Fatalf("files = %+v", files)
	}
	if got := toolDiffFiles(map[string]any{"other": 1}); got != nil {
		t.Fatalf("non-files structured = %+v, want nil", got)
	}
}

// TestWriteDiffs guards the rendered diff: header plus +/- lines.
func TestWriteDiffs(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.writeDiffs([]diffFile{{Path: "a.go", Additions: 1, Deletions: 1, Patch: "@@ -1 +1 @@\n-old\n+new"}})
	})
	for _, want := range []string{"[diff] a.go", "(+1 -1)", "-old", "+new"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestDiffCommandNoSnapshot guards the no-snapshot case.
func TestDiffCommandNoSnapshot(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/diff") {
			t.Error("/diff was not recognised")
		}
	})
	if !strings.Contains(out, "no snapshot") {
		t.Fatalf("out = %q, want the no-snapshot hint", out)
	}
}
