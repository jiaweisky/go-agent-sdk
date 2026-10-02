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
