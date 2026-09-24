package main

import (
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/user/go-agent-sdk/agent"
	"github.com/user/go-agent-sdk/schema"
)

// taskSet tracks the subagent tasks of one chat: byCall maps an open task tool
// call to its session, byID and order list the tasks, and answers caches the
// final text of the tasks that finished.
type taskSet struct {
	byCall  map[string]string
	byID    map[string]*taskRecord
	order   []string
	answers map[string]string
}

func newTaskSet() taskSet {
	return taskSet{
		byCall:  map[string]string{},
		byID:    map[string]*taskRecord{},
		answers: map[string]string{},
	}
}

// taskRecord tracks one subagent task for the lifecycle log and drill-down.
type taskRecord struct {
	index       int
	callID      string
	subagent    string
	description string
	started     time.Time
	tools       int
	// lastTool feeds the bottom status line (the current tool of a busy task).
	lastTool string
}

// taskHandle is the human-facing handle for a task: "#N" by start order.
// Session ids are not usable as a handle — ids created in the same millisecond
// share their first characters, so they would be indistinguishable.
func taskHandle(record *taskRecord) string {
	if record == nil {
		return "#?"
	}
	return fmt.Sprintf("#%d", record.index)
}

// taskOpenLine is the header of a task block, "#N Subagent Task — description".
func taskOpenLine(record *taskRecord) string {
	return fmt.Sprintf("\n%s┌ %s %s Task — %s%s\n",
		yellow, taskHandle(record), titlecase(record.subagent), record.description, reset)
}

// taskCloseLine is the bare footer of a task block (the result line adds detail).
func taskCloseLine(record *taskRecord) string {
	return fmt.Sprintf("%s└ %s%s\n", yellow, taskHandle(record), reset)
}

// skillName extracts the requested skill from the skill tool's input (the
// provider-parsed arguments object, e.g. {"name": "code-review"}).
func skillName(input any) string {
	switch value := input.(type) {
	case map[string]any:
		if name, ok := value["name"].(string); ok {
			return name
		}
	case string:
		return value
	}
	return "?"
}

// formatToolCalls renders a tool-call count for the task result line.
func formatToolCalls(count int) string {
	if count == 1 {
		return "1 toolcall"
	}
	return fmt.Sprintf("%d toolcalls", count)
}

// records iterates the tracked tasks in start order.
func (c *chat) records() iter.Seq2[string, *taskRecord] {
	return func(yield func(string, *taskRecord) bool) {
		for _, childID := range c.tasks.order {
			if record := c.tasks.byID[childID]; record != nil {
				if !yield(childID, record) {
					return
				}
			}
		}
	}
}

// handleProgress registers a task (from the tool's progress metadata) and prints
// its start line.
func (c *chat) handleProgress(event agent.ToolProgress) {
	structured, ok := event.Structured.(map[string]any)
	if !ok {
		return
	}
	childID := stringField(structured, "sessionId")
	subagent := stringField(structured, "subagentType")
	if childID == "" || subagent == "" {
		return
	}
	description := stringField(structured, "description")
	c.tasks.byCall[event.CallID] = childID
	if _, seen := c.tasks.byID[childID]; !seen {
		record := &taskRecord{
			index: len(c.tasks.order) + 1, callID: event.CallID, subagent: subagent,
			description: description, started: time.Now(),
		}
		c.tasks.byID[childID] = record
		c.tasks.order = append(c.tasks.order, childID)
		c.console.write(taskOpenLine(record))
	}
}

// finishTool closes a tool call. Non-task tools print nothing (the status line
// shows the current tool); a task result prints the result preview.
func (c *chat) finishTool(callID, failure string) {
	c.endCall(callID)
	childID, isTask := c.tasks.byCall[callID]
	if !isTask {
		return
	}
	delete(c.tasks.byCall, callID)

	record := c.tasks.byID[childID]
	elapsed, tools := time.Duration(0), 0
	if record != nil {
		elapsed, tools = time.Since(record.started), record.tools
	}
	answer := ""
	if failure == "" {
		// Read fresh: a resumed task reuses its session, so a cached answer would
		// be the previous run's.
		answer = c.subagentAnswer(childID)
		c.tasks.answers[childID] = answer
	}
	mark := "✓"
	if failure != "" {
		mark = "✗"
	}
	detail := elapsed.Round(100 * time.Millisecond).String()
	if tools > 0 {
		detail = formatToolCalls(tools) + " · " + detail
	}
	c.console.write(fmt.Sprintf("%s└ %s %s %s · %s (%s for the full text)%s\n",
		yellow, taskHandle(record), mark, detail, summarize(answer, failure), "/task "+taskHandle(record), reset))
}

// taskCommand handles "/task" (list) and "/task <#n|description>" (full answer).
func (c *chat) taskCommand(input string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	arg := strings.TrimSpace(strings.TrimPrefix(input, "/task"))
	if arg == "" {
		if len(c.tasks.order) == 0 {
			c.console.write("(no tasks)\n")
			return
		}
		for childID, record := range c.records() {
			status := "done"
			if c.busy[childID] {
				status = "running"
			}
			c.console.write(fmt.Sprintf("%s · %s · %s Task — %s\n",
				taskHandle(record), status, titlecase(record.subagent), record.description))
		}
		return
	}
	arg = strings.TrimPrefix(arg, "#")
	for childID, record := range c.records() {
		if arg != fmt.Sprint(record.index) && !strings.HasPrefix(record.description, arg) {
			continue
		}
		c.console.write(taskOpenLine(record))
		c.console.write(renderMarkdown(c.answerFor(childID)))
		c.console.write(taskCloseLine(record))
		return
	}
	c.console.write("task not found: " + arg + "\n")
}

// answerFor returns a subagent's final text: the answer cached when its task
// finished, or a read of its session when the task is still running.
func (c *chat) answerFor(childID string) string {
	if answer := c.tasks.answers[childID]; answer != "" {
		return answer
	}
	return c.subagentAnswer(childID)
}

// subagentAnswer reads the last assistant text of a subagent session.
func (c *chat) subagentAnswer(childID string) string {
	messages, err := c.client.SessionMessages(childID)
	if err != nil {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		assistant, ok := messages[i].(schema.SessionMessageAssistant)
		if !ok {
			continue
		}
		for j := len(assistant.Content) - 1; j >= 0; j-- {
			if text, ok := assistant.Content[j].(schema.SessionMessageAssistantText); ok {
				return text.Text
			}
		}
	}
	return ""
}

// summarize renders the task result preview line.
func summarize(answer, failure string) string {
	if failure != "" {
		return "Summary: failed: " + truncateRunes(firstLine(failure), 80)
	}
	text := strings.TrimSpace(answer)
	if text == "" {
		return "Summary: (no output)"
	}
	return "Summary: " + inline(truncateRunes(firstLine(text), 80))
}

func truncateRunes(s string, limit int) string {
	if runes := []rune(s); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return s
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimRight(text, "\r")
}

// titlecase upper-cases the first rune of s (the subagent name in a task
// title).
func titlecase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func stringField(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}
