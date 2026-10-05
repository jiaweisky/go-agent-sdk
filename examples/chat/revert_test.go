package main

import (
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// TestUndoCommandNothingToUndo guards /undo with no user messages.
func TestUndoCommandNothingToUndo(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/undo") {
			t.Error("/undo was not recognised")
		}
	})
	if !strings.Contains(out, "nothing to undo") {
		t.Fatalf("out = %q, want the nothing-to-undo hint", out)
	}
}

// TestRedoCommandWithoutSession guards /redo with no active session.
func TestRedoCommandWithoutSession(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/redo") {
			t.Error("/redo was not recognised")
		}
	})
	if !strings.Contains(out, "(no active session)") {
		t.Fatalf("out = %q, want the no-session hint", out)
	}
}

// TestRedoCommandNothingToRevert guards /redo when no revert is staged.
func TestRedoCommandNothingToRevert(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/redo") {
			t.Error("/redo was not recognised")
		}
	})
	if !strings.Contains(out, "nothing to redo") {
		t.Fatalf("out = %q, want the nothing-to-redo hint", out)
	}
}

// TestUserMessageStepping guards walking the revert boundary back and forward.
func TestUserMessageStepping(t *testing.T) {
	ids := []string{"a", "b", "c"}
	cases := []struct {
		boundary       string
		prev, next     string
		prevOK, nextOK bool
	}{
		{"", "c", "", true, false},
		{"c", "b", "", true, false},
		{"b", "a", "c", true, true},
		{"a", "", "b", false, true},
		{"x", "", "", false, false},
	}
	for _, tc := range cases {
		if got, ok := previousUserMessage(ids, tc.boundary); got != tc.prev || ok != tc.prevOK {
			t.Errorf("previousUserMessage(%q) = %q, %v; want %q, %v", tc.boundary, got, ok, tc.prev, tc.prevOK)
		}
		if got, ok := nextUserMessage(ids, tc.boundary); got != tc.next || ok != tc.nextOK {
			t.Errorf("nextUserMessage(%q) = %q, %v; want %q, %v", tc.boundary, got, ok, tc.next, tc.nextOK)
		}
	}
	if _, ok := previousUserMessage(nil, ""); ok {
		t.Error("previousUserMessage(nil, \"\") should be empty")
	}
}

// TestRevertDiffFiles guards the revert diff conversion.
func TestRevertDiffFiles(t *testing.T) {
	files := revertDiffFiles([]schema.RevertFileDiff{{Path: "a.go", Additions: 2, Deletions: 1, Patch: "+x"}})
	if len(files) != 1 || files[0].Path != "a.go" || files[0].Additions != 2 || files[0].Deletions != 1 {
		t.Fatalf("files = %+v", files)
	}
}
