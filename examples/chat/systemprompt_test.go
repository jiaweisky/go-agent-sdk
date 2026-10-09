package main

import (
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// TestPersonaCommandReplace guards that /persona --replace accepts the flag and
// still starts a new session.
func TestPersonaCommandReplace(t *testing.T) {
	var session *agent.Session
	out := runCommand(t, nil, func(c *chat) {
		c.personaCommand("/persona --replace Reply only in haiku.")
		session = c.session
	})
	if session == nil {
		t.Fatal("persona --replace did not create a session")
	}
	if !strings.Contains(out, "new persona session") {
		t.Fatalf("out = %q, want the new-session notice", out)
	}
}

// TestResolveSectionOverride locks the /prompt validation matrix: a section is
// always required, remove needs no text, and the editing actions do.
func TestResolveSectionOverride(t *testing.T) {
	cases := []struct {
		action, section, text string
		ok                    bool
	}{
		{"add", "rules", "x", true},
		{"append", "rules", "x", true},
		{"replace", "agent_system", "x", true},
		{"remove", "agent_system", "", true},
		{"add", "rules", "", false},
		{"replace", "rules", "", false},
		{"remove", "", "", false},
		{"add", "", "x", false},
		{"bogus", "rules", "x", false},
	}
	for _, tc := range cases {
		if _, ok := resolveSectionOverride(tc.action, tc.section, tc.text); ok != tc.ok {
			t.Errorf("resolveSectionOverride(%q, %q, %q) ok = %v, want %v",
				tc.action, tc.section, tc.text, ok, tc.ok)
		}
	}
}

// TestPromptCommandUsage guards the no-argument case.
func TestPromptCommandUsage(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/prompt") {
			t.Error("/prompt was not recognised")
		}
	})
	if !strings.Contains(out, "usage: /prompt") {
		t.Fatalf("out = %q, want the usage hint", out)
	}
}

// TestPromptCommandCreatesSession guards that each /prompt form starts a new
// session with a customized system prompt.
func TestPromptCommandCreatesSession(t *testing.T) {
	for _, input := range []string{
		"/prompt add rules Always cite sources.",
		"/prompt replace agent_system You are terse.",
		"/prompt remove agent_system",
	} {
		var session *agent.Session
		out := runCommand(t, nil, func(c *chat) {
			if !handleCommand(c, input) {
				t.Errorf("%q was not recognised", input)
			}
			session = c.session
		})
		if session == nil {
			t.Fatalf("%q did not create a session", input)
		}
		if !strings.Contains(out, "new prompt session") {
			t.Fatalf("%q out = %q, want the new-session notice", input, out)
		}
	}
}
