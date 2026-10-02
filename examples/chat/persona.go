package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/go-agent-sdk"
)

// personaCommand starts a new session whose system prompt is the build agent's
// plus the given text, demonstrating SessionConfig.SystemPrompt. The chat
// switches to the new session and closes the previous one.
func (c *chat) personaCommand(input string) {
	persona := strings.TrimSpace(strings.TrimPrefix(input, "/persona"))
	if persona == "" {
		c.console.write("usage: /persona <text>  (start a new session with an appended system prompt)\n")
		return
	}
	session, err := c.client.CreateSession(context.Background(), &agent.SessionConfig{
		Agent:        agentName,
		SystemPrompt: &agent.SystemPromptConfig{Mode: agent.SystemPromptAppend, Content: persona},
	})
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.replaceSession(session)
	c.console.write(fmt.Sprintf("new persona session %s%s%s\n", cyan, session.ID(), reset))
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
	c.mu.Unlock()
}
