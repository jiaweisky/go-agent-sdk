package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk"
)

// countToParams is the count_to tool's input.
type countToParams struct {
	N int `json:"n" jsonschema:"count from 1 up to this number (1-10, default 5)"`
}

// countToTool counts to N, reporting progress after each step. It is the user
// tool that exercises ToolContext.Progress.
func countToTool() agent.Tool {
	return agent.DefineTool("count_to", "Count from 1 to N (max 10), reporting progress after each step.",
		func(params countToParams, tc agent.ToolContext) (string, error) {
			total := clampSteps(params.N)
			for current := 1; current <= total; current++ {
				if err := reportStep(tc, current, total); err != nil {
					return "", err
				}
				time.Sleep(120 * time.Millisecond)
			}
			return fmt.Sprintf("counted to %d", total), nil
		})
}

// clampSteps bounds count_to's target to 1..10, defaulting to 5.
func clampSteps(n int) int {
	switch {
	case n < 1:
		return 5
	case n > 10:
		return 10
	default:
		return n
	}
}

// reportStep publishes one count_to step, if the tool has a progress context.
func reportStep(tc agent.ToolContext, current, total int) error {
	if tc.Progress == nil {
		return nil
	}
	return tc.Progress(map[string]any{"current": current, "total": total}, nil)
}

// writeToolProgress renders a user tool's running progress line.
func (c *chat) writeToolProgress(structured map[string]any) {
	c.console.write(fmt.Sprintf("%s[progress] %s%s\n", dim, formatProgress(structured), reset))
}

// formatProgress renders progress fields in a stable key order.
func formatProgress(structured map[string]any) string {
	keys := make([]string, 0, len(structured))
	for key := range structured {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, structured[key]))
	}
	return strings.Join(parts, " ")
}
