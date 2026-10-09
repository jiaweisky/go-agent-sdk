package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// startSystemPromptSession creates a session with the given system prompt
// customization and switches the chat to it. The customization only applies at
// creation (it is not persisted), so every edit starts a new session.
func (c *chat) startSystemPromptSession(prompt *agent.SystemPromptConfig, label string) {
	config := c.sessionConfig()
	config.SystemPrompt = prompt
	session, err := c.client.CreateSession(context.Background(), config)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.replaceSession(session)
	c.console.write(fmt.Sprintf("new %s session %s%s%s\n", label, cyan, session.ID(), reset))
}

// promptCommand starts a new session with a customized system prompt: one named
// section is added, replaced, or removed (SessionConfig.SystemPrompt in
// customize mode). The built-in section ids are agent_system (the agent's own
// prompt) and instructions; any other name defines a custom section rendered
// between them.
func (c *chat) promptCommand(input string) {
	action, rest := splitWord(commandArgument(input, "/prompt"))
	section, text := splitWord(rest)
	override, ok := resolveSectionOverride(action, section, text)
	if !ok {
		c.writePromptUsage()
		return
	}
	c.startSystemPromptSession(&agent.SystemPromptConfig{
		Mode:     agent.SystemPromptCustomize,
		Sections: map[string]agent.SectionOverride{section: override},
	}, "prompt")
}

// resolveSectionOverride maps a /prompt action to the section override to apply.
// It reports false for an unknown action, a missing section, or a missing text
// for the editing actions.
func resolveSectionOverride(action, section, text string) (agent.SectionOverride, bool) {
	switch {
	case section == "":
		return agent.SectionOverride{}, false
	case action == "remove":
		return agent.SectionOverride{Action: agent.SectionRemove}, true
	case text == "":
		return agent.SectionOverride{}, false
	case action == "add" || action == "append":
		return agent.SectionOverride{Action: agent.SectionAppend, Content: text}, true
	case action == "replace":
		return agent.SectionOverride{Action: agent.SectionReplace, Content: text}, true
	default:
		return agent.SectionOverride{}, false
	}
}

// writePromptUsage prints the /prompt syntax and the built-in section ids.
func (c *chat) writePromptUsage() {
	c.console.write("usage: /prompt add|replace <section> <text> | remove <section>\n")
	c.console.write("  sections: agent_system, instructions, or any custom name\n")
}

// splitWord splits off the first whitespace-delimited word of s and returns it
// with the trimmed remainder.
func splitWord(s string) (word, rest string) {
	word, rest, _ = strings.Cut(strings.TrimSpace(s), " ")
	return word, strings.TrimSpace(rest)
}
