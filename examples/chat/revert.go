package main

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// undoCommand stages a revert one user message further back: the file changes
// made after that message are restored to their pre-edit snapshots, and the
// reverted message text is echoed so it can be re-sent. Repeated /undo walks back
// through the session's user messages.
func (c *chat) undoCommand() {
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	target, ok := previousUserMessage(c.userMessageIDs(), c.revertBoundary())
	if !ok {
		c.console.write("(nothing to undo)\n")
		return
	}
	if !c.stageRevert(target) {
		return
	}
	if text := c.userMessageText(target); text != "" {
		c.console.write(fmt.Sprintf("%s[undo] back to: %s%s\n", dim, truncateRunes(firstLine(text), 100), reset))
	}
}

// redoCommand walks a staged revert forward one user message, or restores all of
// it when the revert is already at the latest message.
func (c *chat) redoCommand() {
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	boundary := c.revertBoundary()
	if boundary == "" {
		c.console.write("(nothing to redo)\n")
		return
	}
	if next, ok := nextUserMessage(c.userMessageIDs(), boundary); ok {
		c.stageRevert(next)
		return
	}
	if err := c.session.RevertClear(); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write("restored the reverted changes\n")
}

// stageRevert stages a revert to messageID and prints the reverted diff,
// reporting whether it succeeded.
func (c *chat) stageRevert(messageID string) bool {
	revert, err := c.session.RevertStage(messageID)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return false
	}
	files := revertDiffFiles(revert.Files)
	if len(files) == 0 {
		c.console.write("(no file changes to revert)\n")
		return true
	}
	c.writeDiffs(files)
	c.console.write(fmt.Sprintf("%sreverted %d file(s); /redo moves forward%s\n", dim, len(files), reset))
	return true
}

// revertBoundary is the staged revert's boundary message id, or "".
func (c *chat) revertBoundary() string {
	if revert := c.session.Revert(); revert != nil {
		return string(revert.MessageID)
	}
	return ""
}

// userMessageText returns a user message's text, or "".
func (c *chat) userMessageText(id string) string {
	messages, err := c.session.Messages()
	if err != nil {
		return ""
	}
	for _, message := range messages {
		if user, ok := message.(schema.SessionMessageUser); ok && string(user.ID) == id {
			return user.Text
		}
	}
	return ""
}

// previousUserMessage returns the user message before boundary, or the last user
// message when boundary is empty.
func previousUserMessage(ids []string, boundary string) (string, bool) {
	// No boundary means "not yet reverted": start from the last message.
	index := len(ids)
	if boundary != "" {
		index = userMessageIndex(ids, boundary)
	}
	if index-1 < 0 {
		return "", false
	}
	return ids[index-1], true
}

// nextUserMessage returns the user message after boundary, if any.
func nextUserMessage(ids []string, boundary string) (string, bool) {
	index := userMessageIndex(ids, boundary) + 1
	if index <= 0 || index >= len(ids) {
		return "", false
	}
	return ids[index], true
}

// userMessageIndex returns the position of id in ids, or -1.
func userMessageIndex(ids []string, id string) int {
	for i, item := range ids {
		if item == id {
			return i
		}
	}
	return -1
}

// userMessageIDs returns the session's user message ids in order.
func (c *chat) userMessageIDs() []string {
	messages, err := c.session.Messages()
	if err != nil {
		return nil
	}
	ids := make([]string, 0)
	for _, message := range messages {
		if user, ok := message.(schema.SessionMessageUser); ok {
			ids = append(ids, string(user.ID))
		}
	}
	return ids
}

// revertDiffFiles converts revert file diffs into the shared diffFile shape.
func revertDiffFiles(diffs []schema.RevertFileDiff) []diffFile {
	files := make([]diffFile, 0, len(diffs))
	for _, diff := range diffs {
		files = append(files, diffFile{
			Path:      string(diff.Path),
			Additions: int(diff.Additions),
			Deletions: int(diff.Deletions),
			Patch:     diff.Patch,
		})
	}
	return files
}
