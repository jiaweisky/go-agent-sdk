package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/core"
)

// emitLinesTool returns many lines of text, so a large tool result can be
// triggered on demand: over the configured limits the SDK spills the full output
// to a file and shows the model a truncated preview with a marker.
func emitLinesTool() agent.Tool {
	return agent.DefineTool("emit_lines", "Emit N lines of text; use a large N to trigger output truncation.",
		func(input emitLinesInput, _ agent.ToolContext) (string, error) {
			if input.Count < 1 {
				return "", fmt.Errorf("count must be positive")
			}
			var b strings.Builder
			for i := 1; i <= input.Count; i++ {
				fmt.Fprintf(&b, "line %d\n", i)
			}
			return strings.TrimSuffix(b.String(), "\n"), nil
		})
}

type emitLinesInput struct {
	Count int `json:"count" jsonschema:"Number of lines to emit (e.g. 5000 exceeds the 2000-line default)"`
}

// toolOutputCommand handles "/tool-output" (report) and "/tool-output cleanup".
func (c *chat) toolOutputCommand(input string) {
	args := commandFields(input, "/tool-output")
	switch {
	case len(args) == 0:
		c.writeToolOutput()
	case len(args) == 1 && args[0] == "cleanup":
		c.cleanupToolOutput()
	default:
		c.console.write("usage: /tool-output [cleanup]\n")
	}
}

// writeToolOutput reports where oversized tool output is spilled.
func (c *chat) writeToolOutput() {
	dir := c.toolOutputDir()
	c.console.write(fmt.Sprintf("%sTool output:%s\n", bold, reset))
	c.console.write(fmt.Sprintf("  %s%s%s\n", dim, dir, reset))
	c.console.write(fmt.Sprintf("  %d file(s); files older than 7 days are pruned\n", countToolOutputFiles(dir)))
}

// cleanupToolOutput prunes expired spill files and reports the counts:
// Client.CleanupToolOutput reports neither how many files it scanned nor removed.
func (c *chat) cleanupToolOutput() {
	dir := c.toolOutputDir()
	before := countToolOutputFiles(dir)
	if err := c.client.CleanupToolOutput(); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	after := countToolOutputFiles(dir)
	c.console.write(fmt.Sprintf("tool output: scanned %d, removed %d (only files older than 7 days)\n", before, before-after))
}

// toolOutputDir is where the SDK spills oversized tool output. The SDK exposes no
// accessor for it, so this mirrors core.Global's layout under the client's state
// root.
func (c *chat) toolOutputDir() string {
	root := c.dataRoot
	if root == "" {
		root = dataDir()
	}
	global := core.NewGlobal(root, "")
	return filepath.Join(global.Data, core.ToolOutputStoreManagedDirectory)
}

// countToolOutputFiles counts the "tool_*" spill files the SDK manages.
func countToolOutputFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "tool_") {
			count++
		}
	}
	return count
}
