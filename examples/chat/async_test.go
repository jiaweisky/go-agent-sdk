package main

import (
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// TestAsyncCommandToggles guards /async on|off and the status query.
func TestAsyncCommandToggles(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		if !handleCommand(c, "/async on") {
			t.Error("/async was not recognised")
		}
		handleCommand(c, "/async")
		handleCommand(c, "/async off")
	})
	for _, want := range []string{"async interactions on", "async interactions: true", "async interactions off"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestPermissionsCommandEmpty guards the empty pending-permission hint.
func TestPermissionsCommandEmpty(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) { handleCommand(c, "/permissions") })
	if !strings.Contains(out, "no pending permissions") {
		t.Fatalf("out = %q, want the empty hint", out)
	}
}

// TestPermissionAskedRendersAndLists guards that a held permission is rendered
// and listed, and the unknown-selector error.
func TestPermissionAskedRendersAndLists(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.writePermissionAsked(agent.PermissionRequest{
			ID: "per_1", SessionID: "ses_1", Action: "bash", Resources: []string{"ls -la"},
		})
		handleCommand(c, "/permissions")
		handleCommand(c, "/allow 9")
	})
	for _, want := range []string{"[permission] #1 bash ls -la", "/allow 1", "Pending permissions (1):", "unknown pending permission: 9"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}

// TestHandlePermissionEventSync guards that a permission event in sync mode
// neither renders nor deadlocks: chat.handle already holds c.mu, so the event
// handler must not lock it again.
func TestHandlePermissionEventSync(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.handle(agent.PermissionAsked{
			SessionID: "ses_1", Request: agent.PermissionRequest{ID: "per_1", Action: "bash", Resources: []string{"ls"}},
		})
	})
	if strings.Contains(out, "[permission]") {
		t.Fatalf("out = %q, want no render in sync mode", out)
	}
}

// TestHandlePermissionEventAsync guards the event path renders and clears a held
// permission without deadlocking.
func TestHandlePermissionEventAsync(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.setAsync(true)
		c.handle(agent.PermissionAsked{
			SessionID: "ses_1", Request: agent.PermissionRequest{ID: "per_1", Action: "bash", Resources: []string{"ls"}},
		})
		c.handle(agent.PermissionReplied{SessionID: "ses_1", RequestID: "per_1"})
	})
	if !strings.Contains(out, "[permission] #1 bash ls") {
		t.Fatalf("out = %q, want the permission render", out)
	}
}

// TestQuestionAskedRendersAndLists guards that a held question is rendered and
// listed, and that /answer needs a body.
func TestQuestionAskedRendersAndLists(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		c.writeQuestionAsked(agent.QuestionRequest{
			ID: "que_1", SessionID: "ses_1",
			Questions: []schema.QuestionInfo{{Question: "Which database?", Options: []schema.QuestionOption{{Label: "sqlite", Description: "embedded"}}}},
		})
		handleCommand(c, "/questions")
		handleCommand(c, "/answer 1")
	})
	for _, want := range []string{"[question] #1", "Which database?", "1) sqlite - embedded", "Pending questions (1):", "usage: /answer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}
