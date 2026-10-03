package main

import (
	"strings"

	"github.com/user/go-agent-sdk"
)

// personaCommand starts a new session whose system prompt is the build agent's
// plus the given text, or (with --replace) the text alone, using
// SessionConfig.SystemPrompt. It switches the chat to the new session.
func (c *chat) personaCommand(input string) {
	text := commandArgument(input, "/persona")
	replace := false
	if text == "--replace" || strings.HasPrefix(text, "--replace ") {
		replace = true
		text = strings.TrimSpace(strings.TrimPrefix(text, "--replace"))
	}
	if text == "" {
		c.console.write("usage: /persona [--replace] <text>  (start a new session with a system prompt)\n")
		return
	}
	mode := agent.SystemPromptAppend
	if replace {
		mode = agent.SystemPromptReplace
	}
	c.startSystemPromptSession(&agent.SystemPromptConfig{Mode: mode, Content: text}, "persona")
}

// switchSession points the renderer at a new session and resets the per-session
// view state, so tasks and busy flags from the old session do not leak.
func (c *chat) switchSession(session *agent.Session) {
	c.mu.Lock()
	c.session = session
	c.parent = session.ID()
	c.busy = map[string]bool{}
	c.parentBusy = false
	c.calls = map[string]string{}
	c.running = nil
	c.tasks = newTaskSet()
	c.interruptArmed = false
	c.turnStartSnapshot = ""
	c.lastSnapshot = ""
	c.permissions = nil
	c.questions = nil
	c.mu.Unlock()
}
