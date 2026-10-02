package main

import (
	"strings"
	"testing"
)

// TestAgentCommandSwitches guards that /agent switches the active session's
// agent in place.
func TestAgentCommandSwitches(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/agent plan") {
			t.Error("/agent was not recognised")
		}
	})
	if !strings.Contains(out, "agent set to plan") {
		t.Fatalf("out = %q, want the switch notice", out)
	}
}

// TestResolveDefaultAgent guards that a persisted primary agent is reused while
// an unknown one falls back to the build agent.
func TestResolveDefaultAgent(t *testing.T) {
	runCommand(t, nil, func(c *chat) {
		if got := resolveDefaultAgent(c.client, "plan"); got != "plan" {
			t.Errorf("plan = %q, want plan", got)
		}
		if got := resolveDefaultAgent(c.client, "nope"); got != agentName {
			t.Errorf("nope = %q, want %q", got, agentName)
		}
	})
}

// TestAgentAddCommand guards that /agent add registers an agent at runtime and
// that /agents (which lists subagents) shows it immediately.
func TestAgentAddCommand(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/agent add helper a demo helper") {
			t.Error("/agent add was not recognised")
		}
		c.console.write("--list--\n")
		if !handleCommand(c, "/agents") {
			t.Error("/agents was not recognised")
		}
	})
	for _, want := range []string{
		"registered agent helper (subagent, hidden, steps=2)",
		"helper — a demo helper",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestAgentAddCommandExisting guards that /agent add refuses to clobber an
// existing agent (which RegisterAgent would otherwise update in place).
func TestAgentAddCommandExisting(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		if !handleCommand(c, "/agent add build") {
			t.Error("/agent add was not recognised")
		}
		c.console.write("--list--\n")
		handleCommand(c, "/agent")
	})
	if !strings.Contains(out, "agent build already exists (pick another name)") {
		t.Fatalf("out = %q, want the exists error", out)
	}
	list := out[strings.Index(out, "--list--"):]
	if !strings.Contains(list, "build") {
		t.Fatalf("list = %q, build should still be a primary agent", list)
	}
}

// TestAgentAddCommandUsage guards the no-argument case.
func TestAgentAddCommandUsage(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/agent add") {
			t.Error("/agent add was not recognised")
		}
	})
	if !strings.Contains(out, "usage: /agent add") {
		t.Fatalf("out = %q, want the usage hint", out)
	}
}

// TestAgentCommandListAndUnknown guards the list and the unknown-agent error.
func TestAgentCommandListAndUnknown(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		activeSession(t, c)
		handleCommand(c, "/agent")
		c.console.write("--unknown--\n")
		handleCommand(c, "/agent nope")
	})
	for _, want := range []string{"Agents (", "build", "plan", "unknown agent: nope"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}
