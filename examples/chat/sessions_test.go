package main

import (
	"context"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
)

// TestNewCommandCreatesSession guards that /new (and its /clear alias) starts a
// fresh session and switches the chat to it.
func TestNewCommandCreatesSession(t *testing.T) {
	var session *agent.Session
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/new") {
			t.Error("/new was not recognised")
		}
		session = c.session
		c.console.write("--clear--\n")
		if !handleCommand(c, "/clear") {
			t.Error("/clear was not recognised")
		}
	})
	if session == nil {
		t.Fatal("/new did not create a session")
	}
	if !strings.Contains(out, "new session") {
		t.Fatalf("out = %q, want the new-session notice", out)
	}
}

// TestNewCommandClosesPrevious guards that /new detaches the previous session,
// so it can be resumed again (an attached session cannot be resumed twice).
func TestNewCommandClosesPrevious(t *testing.T) {
	ctx := context.Background()
	var first *agent.Session
	out := runCommand(t, nil, func(c *chat) {
		session, err := c.client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		first = session
		c.switchSession(session)

		handleCommand(c, "/new")
		if c.session.ID() == first.ID() {
			t.Fatal("/new did not switch to a new session")
		}
		if _, err := c.client.ResumeSession(ctx, first.ID(), nil); err != nil {
			t.Fatalf("previous session was not detached: %v", err)
		}
	})
	if first == nil || !strings.Contains(out, "new session") {
		t.Fatalf("out = %q, want the new-session notice", out)
	}
}

// TestSessionsCommandListAndSwitch guards the list and the switch by id.
func TestSessionsCommandListAndSwitch(t *testing.T) {
	ctx := context.Background()
	var first *agent.Session
	out := runCommand(t, nil, func(c *chat) {
		session, err := c.client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		first = session
		c.switchSession(first)
		handleCommand(c, "/new") // second session active, first detached

		c.console.write("--list--\n")
		if !handleCommand(c, "/sessions") {
			t.Error("/sessions was not recognised")
		}
		c.console.write("--switch--\n")
		if !handleCommand(c, "/resume "+first.ID()) {
			t.Error("/resume was not recognised")
		}
		if c.session.ID() != first.ID() {
			t.Fatalf("active session = %s, want %s", c.session.ID(), first.ID())
		}
	})
	for _, want := range []string{
		"Sessions (2):",
		"--switch--",
		"switched to session " + first.ID(),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestSessionsCommandEmpty guards the no-session list and an unknown argument.
func TestSessionsCommandEmpty(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		handleCommand(c, "/sessions")
		c.console.write("--missing--\n")
		handleCommand(c, "/sessions nope")
	})
	if !strings.Contains(out, "(no sessions)") {
		t.Fatalf("out = %q, want the empty-sessions hint", out)
	}
	if !strings.Contains(out, "session not found: nope") {
		t.Fatalf("out = %q, want the not-found hint", out)
	}
}

// TestSessionsCommandSwitchByIndexAndAlias guards the "#n" form and the
// /continue alias, which resolve to the single active session.
func TestSessionsCommandSwitchByIndexAndAlias(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		session, err := c.client.CreateSession(context.Background(), &agent.SessionConfig{Agent: "build"})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		c.switchSession(session)

		if !handleCommand(c, "/sessions #1") {
			t.Error("/sessions #1 was not recognised")
		}
		if !handleCommand(c, "/continue #1") {
			t.Error("/continue was not recognised")
		}
	})
	if !strings.Contains(out, "(already the active session)") {
		t.Fatalf("out = %q, want the already-active hint", out)
	}
}

// TestDeleteCommand guards deleting the active session: a fresh session is
// started first, so the chat never points at the removed session.
func TestDeleteCommand(t *testing.T) {
	ctx := context.Background()
	var deleted *agent.Session
	out := runCommand(t, nil, func(c *chat) {
		session, err := c.client.CreateSession(ctx, &agent.SessionConfig{Agent: "build"})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		deleted = session
		c.switchSession(session)

		if !handleCommand(c, "/delete #1") {
			t.Error("/delete was not recognised")
		}
		if c.session != nil && c.session.ID() == deleted.ID() {
			t.Fatal("the chat still points at the deleted session")
		}
	})
	if !strings.Contains(out, "deleted session "+deleted.ID()) {
		t.Fatalf("out = %q, missing the delete notice", out)
	}
}

// TestDeleteCommandUsage guards the no-argument case.
func TestDeleteCommandUsage(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/delete") {
			t.Error("/delete was not recognised")
		}
	})
	if !strings.Contains(out, "usage: /delete") {
		t.Fatalf("out = %q, want the usage hint", out)
	}
}
