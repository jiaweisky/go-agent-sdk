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
	line := c.statusLine(frame)
	if line == "" {
		c.console.setStatus("")
		return
	}
	c.console.setStatus(dim + line + reset)
}

// statusLine renders what is running plus the usage totals, joined for the
// bottom line.
func (c *chat) statusLine(frame int) string {
	parts := make([]string, 0, 2)
	if work := c.workLabel(frame); work != "" {
		parts = append(parts, work)
	}
	if usage := c.usageLine(); usage != "" {
		parts = append(parts, usage)
	}
	return strings.Join(parts, " · ")
}

// workLabel names what is running: the current tool of every busy task, or the
// parent's current tool when only the parent is busy.
func (c *chat) workLabel(frame int) string {
	if parts := c.taskLabels(); len(parts) > 0 {
		return spinner(frame) + " " + strings.Join(parts, " · ")
	}
	if !c.parentBusy {
		return ""
	}
	label := "working"
	if tool := c.currentTool(); tool != "" {
		label = tool
	}
	return spinner(frame) + " " + label
}

// taskLabels names the current tool of every busy task, in start order.
func (c *chat) taskLabels() []string {
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
	return parts
}

// spinner returns the glyph for a frame, or "" when there are no frames.
func spinner(frame int) string {
	if len(spinnerFrames) == 0 {
		return ""
	}
	return spinnerFrames[frame%len(spinnerFrames)]
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
