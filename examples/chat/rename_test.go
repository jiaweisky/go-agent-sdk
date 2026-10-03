package main

import (
	"context"
	"strings"
	"testing"
)

// TestRenameCommandSetsTitle guards that /rename persists the title through
// Session.SetTitle, so the next ListSessions sees it.
func TestRenameCommandSetsTitle(t *testing.T) {
	runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/rename Quarterly plan") {
			t.Error("/rename was not recognised")
		}
		infos, err := c.client.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		if len(infos) != 1 || infos[0].Title != "Quarterly plan" {
			t.Fatalf("sessions = %+v, want one titled %q", infos, "Quarterly plan")
		}
	})
}

// TestRenameCommandRequiresTitle guards the usage hint.
func TestRenameCommandRequiresTitle(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/rename") {
			t.Error("/rename was not recognised")
		}
	})
	if !strings.Contains(out, "usage: /rename") {
		t.Fatalf("out = %q, want the usage hint", out)
	}
}
