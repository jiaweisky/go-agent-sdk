package main

import (
	"fmt"

	"github.com/user/go-agent-sdk/schema"
)

// historyCommand prints the session's stored messages, demonstrating
// Session.Messages: the projected history, which starts at the latest
// compaction and drops aborted/empty assistant turns.
func (c *chat) historyCommand() {
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	messages, err := c.session.Messages()
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	if len(messages) == 0 {
		c.console.write("(no history)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sHistory (%d):%s\n", bold, len(messages), reset))
	for index, message := range messages {
		c.console.write(fmt.Sprintf("  %s%2d%s %-11s %s\n",
			dim, index+1, reset, message.SessionMessageType(), historyPreview(message)))
	}
}

// historyPreview renders one message as a short single-line preview.
func historyPreview(message schema.SessionMessage) string {
	switch m := message.(type) {
	case schema.SessionMessageUser:
		return previewText(m.Text)
	case schema.SessionMessageAssistant:
		for _, part := range m.Content {
			if text, ok := part.(schema.SessionMessageAssistantText); ok && text.Text != "" {
				return previewText(text.Text)
			}
		}
		return "(no text)"
	case schema.SessionMessageCompaction:
		return fmt.Sprintf("summary %d chars, recent %d chars", len(m.Summary), len(m.Recent))
	case schema.SessionMessageSystem:
		return previewText(m.Text)
	case schema.SessionMessageSynthetic:
		return previewText(m.Text)
	case schema.SessionMessageShell:
		return previewText(m.Command)
	}
	return ""
}

// previewText trims a message to a single short line.
func previewText(text string) string {
	return truncateRunes(firstLine(text), 80)
}
