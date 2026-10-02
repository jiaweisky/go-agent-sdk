package main

import (
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
)

// TestSessionHooksPromptRewriteAndDeny guards the two hooks with observable
// behavior: a "?..." prompt is rewritten, and an obvious rm -rf is denied.
func TestSessionHooksPromptRewriteAndDeny(t *testing.T) {
	var out strings.Builder
	hooks := newSessionHooks(&console{out: &out, lineStart: true})

	rewritten, err := hooks.OnUserPromptSubmitted(agent.UserPromptSubmittedInput{Prompt: "?say hi"})
	if err != nil {
		t.Fatalf("prompt hook: %v", err)
	}
	if rewritten == nil || rewritten.ModifiedPrompt != "Answer concisely: say hi" {
		t.Fatalf("rewrite = %+v, want the concise instruction", rewritten)
	}
	if pass, _ := hooks.OnUserPromptSubmitted(agent.UserPromptSubmittedInput{Prompt: "say hi"}); pass != nil {
		t.Fatalf("plain prompt was rewritten: %+v", pass)
	}

	denied, err := hooks.OnPreToolUse(agent.PreToolUseInput{
		ToolName: "bash", Args: map[string]any{"command": "rm -rf /"},
	})
	if err != nil {
		t.Fatalf("pre-tool hook: %v", err)
	}
	if denied == nil || denied.PermissionDecision != "deny" {
		t.Fatalf("rm -rf = %+v, want deny", denied)
	}
	if pass, _ := hooks.OnPreToolUse(agent.PreToolUseInput{ToolName: "read"}); pass != nil {
		t.Fatalf("a normal call was altered: %+v", pass)
	}

	if !strings.Contains(out.String(), "[hook]") {
		t.Fatalf("out = %q, want hook log lines", out.String())
	}
}

// TestSessionHooksSessionStartContext guards that OnSessionStart injects context.
func TestSessionHooksSessionStartContext(t *testing.T) {
	var out strings.Builder
	hooks := newSessionHooks(&console{out: &out, lineStart: true})

	start, err := hooks.OnSessionStart(agent.SessionStartInput{SessionID: "ses_1", Agent: "build", Model: "m"})
	if err != nil {
		t.Fatalf("session start hook: %v", err)
	}
	if start == nil || !strings.Contains(start.AdditionalContext, "current date") {
		t.Fatalf("start = %+v, want date context", start)
	}
}
