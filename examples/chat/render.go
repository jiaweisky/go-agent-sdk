package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/jiaweisky/go-agent-sdk"
	coretool "github.com/jiaweisky/go-agent-sdk/core/tool"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// chat renders client-wide events. Subagent internals are not streamed inline;
// each task shows a lifecycle line plus a one-line result preview, and the full
// answer is available via /task. Every session other than the parent's is a
// subagent's, including the sessions of the tasks a subagent starts.
type chat struct {
	console *console
	client  *agent.Client
	mu      sync.Mutex
	parent  string
	model   string
	// state is the persisted selection (model/agent); statePath is where it is
	// stored, empty when the chat is not persisting (tests).
	state      chatState
	statePath  string
	usage      chatUsage
	busy       map[string]bool
	parentBusy bool
	// session is the active session; the session commands (/new, /sessions,
	// /delete, /persona) replace it, so chatLoop reads it from here instead of
	// a fixed local.
	session *agent.Session
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
	// step counts the parent turn's provider attempts, so a multi-step turn is
	// shown as separated steps.
	step int
	// interruptArmed is set after a first Esc, so the status line prompts for a
	// second press to interrupt the running turn.
	interruptArmed bool
	// tools holds the unregister functions of the runtime tools added with
	// /tool, keyed by name.
	tools map[string]func()
	// plugins tracks the plugins the chat loaded, so /plugin remove can tell an
	// unknown or already-removed id from an active one.
	plugins *pluginRegistry
	// dataRoot is the client's state root (root/.data), so /tool-output can find
	// the SDK's spilled-output directory.
	dataRoot string
	// attachments are images queued with /attach; they are sent with the next
	// message and then cleared.
	attachments []schema.PromptFileAttachment
	// todos caches the parent session's task list, from todo.updated events, so
	// /todos can show it on demand.
	todos map[string][]schema.SessionTodoInfo
	// turnStartSnapshot and lastSnapshot are the current turn's snapshot ids,
	// captured by the chat around the turn. They are not taken from step events
	// because a failed or interrupted turn emits no final step.ended.
	turnStartSnapshot string
	lastSnapshot      string
	// permissions and questions cache pending out-of-band requests (hold mode)
	// in arrival order, so /permissions and /allow can resolve them.
	permissions []agent.PermissionRequest
	questions   []agent.QuestionRequest
	// async is the current /async setting (out-of-band interaction handling).
	async bool
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
		tools:   map[string]func(){},
		plugins: newPluginRegistry(nil),
		todos:   map[string][]schema.SessionTodoInfo{},
	}
}

func (c *chat) beginTurn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Flush text an aborted turn left buffered, then start a fresh stream so its
	// markdown state (an open fence, say) cannot leak into this turn.
	c.flushStream()
	c.stream = nil
	c.labeled = false
	c.step = 0
	c.turnStartSnapshot = ""
	c.lastSnapshot = ""
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
	// A status line must not appear before text that streamed earlier.
	switch event.(type) {
	case agent.ToolProgress, agent.ToolCalled, agent.ToolSuccess, agent.ToolFailed,
		agent.CompactionStarted, agent.CompactionEnded,
		agent.TitleUpdated, agent.Retried,
		agent.ShellStarted, agent.ShellEnded,
		agent.StepStarted, agent.StepEnded, agent.StepFailed,
		agent.TodoUpdated,
		agent.PermissionAsked, agent.PermissionReplied,
		agent.QuestionAsked, agent.QuestionReplied, agent.QuestionRejected:
		c.flushStream()
	}
	switch e := event.(type) {
	case agent.ToolProgress:
		c.handleProgress(e)
	case agent.ToolCalled:
		c.openCall(e.CallID, e.Name)
		if e.Name == coretool.SkillName {
			c.writeSkill(e.Input)
		}
	case agent.ToolSuccess:
		c.finishTool(e.CallID, "")
		c.writeToolDiff(e.Structured)
	case agent.ToolFailed:
		c.finishTool(e.CallID, errorMessage(e.Error))
	case agent.SessionStatus:
		c.parentBusy = e.Status == "busy"
	case agent.TitleUpdated:
		c.writeTitle(e.Title)
	case agent.TodoUpdated:
		c.todos[e.SessionID] = e.Todos
		c.writeTodos(e.Todos)
	case agent.PermissionAsked:
		if c.async {
			c.writePermissionAsked(e.Request)
		}
	case agent.PermissionReplied:
		c.removePermission(e.RequestID)
	case agent.QuestionAsked:
		if c.async {
			c.writeQuestionAsked(e.Request)
		}
	case agent.QuestionReplied:
		c.removeQuestion(e.RequestID)
	case agent.QuestionRejected:
		c.removeQuestion(e.RequestID)
	case agent.StepStarted:
		c.beginStep()
	case agent.StepEnded:
		c.endStep(e.Finish)
		c.writeStepFiles(e.Files)
	case agent.StepFailed:
		c.failStep(e.Error)
	case agent.Retried:
		c.console.write(fmt.Sprintf("%s[retry %v] %s%s\n", dim, e.Attempt, errorMessage(e.Error), reset))
	case agent.ShellStarted:
		c.console.write(fmt.Sprintf("%s[shell] $ %s%s\n", dim, e.Command, reset))
	case agent.ShellEnded:
		if output := shellOutput(e.Output); output != "" {
			c.console.write(dim + output + reset + "\n")
		}
	case agent.CompactionStarted:
		c.console.write(fmt.Sprintf("%s[compact] summarizing history...%s\n", dim, reset))
	case agent.CompactionEnded:
		c.console.write(fmt.Sprintf("%s[compact] summary:%s\n", dim, reset))
		if e.Text != "" {
			c.console.write(renderMarkdown(e.Text))
		}
	case agent.ReasoningDelta:
		c.writeReasoning(e.Delta)
	case agent.ReasoningEnded:
		c.endReasoning(e.Text)
	case agent.TextDelta:
		c.writeText(e.Delta)
	case agent.TextEnded:
		c.endText()
	}
}

// writeTitle surfaces the generated session title.
func (c *chat) writeTitle(title string) {
	if title != "" {
		c.console.write(fmt.Sprintf("%s[title] %s%s\n", cyan, title, reset))
	}
}

// writeTodos prints the task list the model maintains with the todowrite tool.
func (c *chat) writeTodos(todos []schema.SessionTodoInfo) {
	if len(todos) == 0 {
		c.console.write(fmt.Sprintf("%s[todo]%s (cleared)\n", dim, reset))
		return
	}
	for _, todo := range todos {
		c.console.write(fmt.Sprintf("%s[todo]%s %s %s\n", dim, reset, todoMark(todo.Status), todo.Content))
	}
}

// todoMark is the status marker for a todo item.
func todoMark(status string) string {
	switch status {
	case "completed":
		return "☑"
	case "in_progress":
		return "▶"
	case "cancelled":
		return "✕"
	default:
		return "☐"
	}
}

// beginStep separates a turn's provider attempts. The first step is the turn
// start, so it is not labeled; later steps mark the loop's continuation after
// tool results.
func (c *chat) beginStep() {
	c.step++
	if c.step == 1 {
		return
	}
	c.console.write(fmt.Sprintf("%s[step %d]%s\n", dim, c.step, reset))
}

// endStep reports a provider attempt's finish only when it is abnormal (not a
// normal stop or a tool-call handoff), so truncation and filtering are visible.
func (c *chat) endStep(finish any) {
	reason := fmt.Sprint(finish)
	switch reason {
	case "", "stop", "tool-calls", "unknown":
		return
	}
	c.console.write(fmt.Sprintf("%s[step %d: %s]%s\n", dim, c.step, reason, reset))
}

// failStep reports a provider attempt that failed; the turn's error follows.
func (c *chat) failStep(err any) {
	c.console.write(fmt.Sprintf("%s[step %d failed] %s%s\n", yellow, c.step, errorMessage(err), reset))
}

// shellOutput renders a shell.ended output payload as text.
func shellOutput(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(value)
	}
}

// writeSkill surfaces a loaded skill: unlike read/grep, it changes how the model
// behaves for the rest of the turn.
func (c *chat) writeSkill(input any) {
	c.console.write(fmt.Sprintf("%s[skill] %s%s\n", cyan, skillName(input), reset))
}

// writeReasoning streams the model's reasoning, dimmed.
func (c *chat) writeReasoning(delta string) {
	if delta != "" {
		c.console.write(dim + delta + reset)
	}
}

// endReasoning closes a reasoning block that printed anything.
func (c *chat) endReasoning(text string) {
	if text != "" {
		c.console.write("\n")
	}
}

// writeText streams assistant text, printing the "Assistant:" label once per
// turn.
func (c *chat) writeText(delta string) {
	if delta == "" {
		return
	}
	if !c.labeled {
		c.console.write("Assistant: ")
		c.labeled = true
	}
	if c.stream == nil {
		c.stream = newMarkdownStream(c.console.write)
	}
	c.stream.Write(delta)
}

// endText flushes the assistant text and ends its line.
func (c *chat) endText() {
	if c.stream != nil {
		c.stream.Flush()
	}
	c.console.write("\n")
}

// openCall records a running tool call of the parent session. Task calls are
// excluded: the status line names the task itself (see taskSet).
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
