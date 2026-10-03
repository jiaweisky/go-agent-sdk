package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// forkCommand copies the active session's history into a new session and
// switches to it. With "#n" it forks before the n-th user message, dropping that
// message and everything after it.
func (c *chat) forkCommand(input string) {
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	messageID, ok := c.forkBoundary(commandArgument(input, "/fork"))
	if !ok {
		return
	}
	forked, err := c.client.ForkSession(context.Background(), c.parent, messageID)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.switchSession(forked)
	c.console.write(fmt.Sprintf("%sforked to %s%s\n", dim, forked.ID(), reset))
}

// forkBoundary resolves an optional "#n" user-message selector into the fork
// boundary: the chosen user message id, so the fork keeps everything before it.
func (c *chat) forkBoundary(arg string) (string, bool) {
	if arg == "" {
		return "", true
	}
	ids := c.userMessageIDs()
	index, err := strconv.Atoi(strings.TrimPrefix(arg, "#"))
	if err != nil || index < 1 || index > len(ids) {
		c.console.write(fmt.Sprintf("unknown user message: %s (have 1..%d)\n", arg, len(ids)))
		return "", false
	}
	return ids[index-1], true
}
