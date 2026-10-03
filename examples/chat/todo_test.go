package main

import (
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

// TestTodosCommand guards that /todos prints the cached task list and the empty
// case.
func TestTodosCommand(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) {
		handleCommand(c, "/todos")
		c.mu.Lock()
		c.todos[c.parent] = []schema.SessionTodoInfo{
			{Content: "read code", Status: "in_progress", Priority: "high"},
			{Content: "run tests", Status: "pending", Priority: "medium"},
		}
		c.mu.Unlock()
		c.console.write("--list--\n")
		handleCommand(c, "/todos")
	})
	for _, want := range []string{"(no todos", "--list--", "[todo]", "read code", "run tests"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out = %q, missing %q", out, want)
		}
	}
}
