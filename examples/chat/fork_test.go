package main

import (
	"strings"
	"testing"
)

// TestForkCommandSwitchesSession guards that /fork copies the session into a new
// one and switches the chat to it.
func TestForkCommandSwitchesSession(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		before := c.session.ID()
		if !handleCommand(c, "/fork") {
			t.Error("/fork was not recognised")
		}
		if c.session == nil || c.session.ID() == before {
			t.Fatal("fork did not switch to a new session")
		}
	})
	if !strings.Contains(out, "forked to") {
		t.Fatalf("out = %q, want the fork notice", out)
	}
}

// TestForkCommandUnknownMessage guards the "#n" selector bounds.
func TestForkCommandUnknownMessage(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/fork #3") {
			t.Error("/fork was not recognised")
		}
	})
	if !strings.Contains(out, "unknown user message") {
		t.Fatalf("out = %q, want the selector error", out)
	}
}

// TestForkCommandWithoutSession guards /fork with no active session.
func TestForkCommandWithoutSession(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/fork") {
			t.Error("/fork was not recognised")
		}
	})
	if !strings.Contains(out, "(no active session)") {
		t.Fatalf("out = %q, want the no-session hint", out)
	}
}
