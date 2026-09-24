package main

import (
	"fmt"
	"strings"
)

// spinnerFrames animates the bottom status line.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// updateStatus redraws the bottom status line: an animated spinner plus the
// current tool of every busy subagent, then the accumulated usage and cost.
// frame advances on each tick so the spinner animates.
func (c *chat) updateStatus(frame int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drawStatusLocked(frame)
}

// drawStatusLocked must be called with c.mu held.
func (c *chat) drawStatusLocked(frame int) {
	spinner := ""
	if len(spinnerFrames) > 0 {
		spinner = spinnerFrames[frame%len(spinnerFrames)]
	}
	parts := make([]string, 0, len(c.tasks.order))
	for childID, record := range c.records() {
		if !c.busy[childID] {
			continue
		}
		label := "working"
		if record.lastTool != "" {
			label = record.lastTool
		}
		parts = append(parts, fmt.Sprintf("%s %s", taskHandle(record), label))
	}
	line := ""
	switch {
	case len(parts) > 0:
		line = fmt.Sprintf("%s %s", spinner, strings.Join(parts, " · "))
	case c.parentBusy:
		label := "working"
		if tool := c.currentTool(); tool != "" {
			label = tool
		}
		line = fmt.Sprintf("%s %s", spinner, label)
	}
	if usage := c.usageLine(); usage != "" {
		if line != "" {
			line += " · "
		}
		line += usage
	}
	if line == "" {
		c.console.setStatus("")
		return
	}
	c.console.setStatus(dim + line + reset)
}

// usageLine renders the accumulated token/cost totals.
func (c *chat) usageLine() string {
	if c.usage.tokens() == 0 {
		return ""
	}
	line := formatTokens(c.usage.tokens()) + " tok"
	// Only a priced model has a cost to show; otherwise the tokens stand alone
	// instead of being reported as ¥0.
	if c.usage.priced {
		line += fmt.Sprintf(" · ¥%.4f", c.usage.cost)
	}
	return line
}

// clearStatus resets the run state after a turn and redraws the status line so
// the accumulated usage/cost stays visible (without the spinner).
func (c *chat) clearStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = map[string]bool{}
	c.parentBusy = false
	c.calls = map[string]string{}
	c.running = nil
	c.drawStatusLocked(0)
}
