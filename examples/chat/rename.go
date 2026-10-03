package main

import "fmt"

// renameCommand sets the active session's title via Session.SetTitle, which
// records a title-updated event. The chat renders the new title from that event,
// so this only reports usage or a failure.
func (c *chat) renameCommand(input string) {
	title := commandArgument(input, "/rename")
	if title == "" {
		c.console.write("usage: /rename <title>\n")
		return
	}
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	if err := c.session.SetTitle(title); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
	}
}
