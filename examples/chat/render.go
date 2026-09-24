package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/agent"
	coretool "github.com/user/go-agent-sdk/core/tool"
)

// chat renders client-wide events. Subagent internals are not streamed inline;
// each task shows a lifecycle line plus a one-line result preview, and the full
// answer is available via /task. Every session other than the parent's is a
// subagent's, including the sessions of the tasks a subagent starts.
type chat struct {
	console    *console
	client     *agent.Client
	mu         sync.Mutex
	parent     string
	model      string
	usage      chatUsage
	busy       map[string]bool
	parentBusy bool
	// tasks tracks the subagent tasks this chat started.
	tasks taskSet
	// calls maps an open parent tool call to its tool name, and running holds
	// the ids of its non-task calls in start order: the tool calls of one step
	// run in parallel, so the status line names the most recent call that is
	// still running rather than one field that any finishing call would clear.
	calls   map[string]string
	running []string
	// stream is the parent's assistant text stream; labeled records whether its
	// "Assistant:" prefix was printed for this turn.
	stream  *markdownStream
	labeled bool
}

func newChat(console *console, client *agent.Client, parent, model string) *chat {
	return &chat{
		console: console,
		client:  client,
		parent:  parent,
		model:   model,
		busy:    map[string]bool{},
		tasks:   newTaskSet(),
		calls:   map[string]string{},
	}
}

func (c *chat) beginTurn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.labeled = false
}

func (c *chat) handle(event agent.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Usage is turn-scoped and provider-raw: fold it into the totals ourselves.
	if usage, ok := event.(agent.Usage); ok {
		c.usage.addUsage(c.model, usage.ProviderMetadata, time.Now())
		return
	}
	sessionID := agent.SessionIDOf(event)
	if sessionID == "" {
		// Events without a session id (retries, steps) belong to the parent's
		// turn.
		sessionID = c.parent
	}
	if sessionID == c.parent {
		c.handleParent(event)
		return
	}
	// Every other session is a subagent's: a task's session, or the session of a
	// task started by one (the SDK allows nesting). Classifying by "not the
	// parent's session" covers grandchildren too, instead of rendering their
	// text as the user's own assistant reply.
	c.handleChild(event, sessionID)
}

// handleChild tracks one subagent session. A task started by a subagent gets its
// own lifecycle lines, and the subagent's current tool feeds the bottom status
// line; nothing else is printed: per-tool-call lines (including failures — the
// model often gets arguments wrong) are noise and the subagent's text belongs to
// /task.
func (c *chat) handleChild(event agent.Event, sessionID string) {
	switch e := event.(type) {
	case agent.ToolProgress:
		c.handleProgress(e)
	case agent.ToolCalled:
		record := c.tasks.byID[sessionID]
		if record == nil || e.Name == agent.TaskName {
			return
		}
		record.tools++
		record.lastTool = e.Name
	case agent.ToolSuccess:
		c.finishTool(e.CallID, "")
	case agent.ToolFailed:
		c.finishTool(e.CallID, errorMessage(e.Error))
	case agent.SessionStatus:
		// Authoritative run state from the SDK's session.status event.
		if e.Status == "busy" {
			c.busy[sessionID] = true
		} else {
			delete(c.busy, sessionID)
		}
	}
}

// flushStream writes any buffered (partial) assistant text so a following tool
// or task line does not appear before text that streamed earlier.
func (c *chat) flushStream() {
	if c.stream != nil {
		c.stream.Flush()
	}
}

func (c *chat) handleParent(event agent.Event) {
	// A tool or task line must not appear before text that streamed earlier.
	switch event.(type) {
	case agent.ToolProgress, agent.ToolCalled, agent.ToolSuccess, agent.ToolFailed:
		c.flushStream()
	}
	switch e := event.(type) {
	case agent.ToolProgress:
		c.handleProgress(e)
	case agent.ToolCalled:
		c.openCall(e.CallID, e.Name)
		if e.Name == coretool.SkillName {
			// Skills are worth surfacing: unlike read/grep, a loaded skill
			// changes how the model behaves for the rest of the turn.
			c.console.write(fmt.Sprintf("%s[skill] %s%s\n", cyan, skillName(e.Input), reset))
		}
	case agent.ToolSuccess:
		c.finishTool(e.CallID, "")
	case agent.ToolFailed:
		c.finishTool(e.CallID, errorMessage(e.Error))
	case agent.SessionStatus:
		c.parentBusy = e.Status == "busy"
	case agent.ReasoningDelta:
		if e.Delta != "" {
			c.console.write(dim + e.Delta + reset)
		}
	case agent.ReasoningEnded:
		if e.Text != "" {
			c.console.write("\n")
		}
	case agent.TextDelta:
		if e.Delta == "" {
			return
		}
		if !c.labeled {
			c.console.write("Assistant: ")
			c.labeled = true
		}
		if c.stream == nil {
			c.stream = newMarkdownStream(c.console.write)
		}
		c.stream.Write(e.Delta)
	case agent.TextEnded:
		if c.stream != nil {
			c.stream.Flush()
		}
		c.console.write("\n")
	}
}

// openCall records a running tool call of the parent session. Task calls are
// excluded: the status line names the task itself (see tasks).
func (c *chat) openCall(callID, name string) {
	if name == agent.TaskName {
		return
	}
	c.calls[callID] = name
	c.running = append(c.running, callID)
}

// endCall drops a finished tool call.
func (c *chat) endCall(callID string) {
	delete(c.calls, callID)
	for i, id := range c.running {
		if id == callID {
			c.running = append(c.running[:i], c.running[i+1:]...)
			return
		}
	}
}

// currentTool is the parent's most recently started tool call that is still
// running: tool calls in one step run in parallel, so a finishing call must not
// clear the label of a call that is still going.
func (c *chat) currentTool() string {
	for i := len(c.running) - 1; i >= 0; i-- {
		if name, ok := c.calls[c.running[i]]; ok {
			return name
		}
	}
	return ""
}

// errorMessage extracts a readable message from a tool error payload.
func errorMessage(value any) string {
	switch v := value.(type) {
	case nil:
		return "failed"
	case string:
		return v
	case map[string]any:
		if message, ok := v["message"].(string); ok && message != "" {
			return message
		}
	}
	return fmt.Sprint(value)
}
