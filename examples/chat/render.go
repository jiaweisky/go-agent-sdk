package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/agent"
	coretool "github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/schema"
)

const (
	cyan      = "\033[36m"
	bold      = "\033[1m"
	italic    = "\033[3m"
	underline = "\033[4m"
)

// markdown is a minimal, line-based markdown → ANSI renderer with fence state.
type markdown struct {
	inFence bool
}

func (m *markdown) render(line string) string {
	trimmed := strings.TrimRight(line, " \t")
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		m.inFence = !m.inFence
		return dim + trimmed + reset
	}
	if m.inFence {
		return cyan + line + reset
	}
	if _, text, ok := heading(trimmed); ok {
		return bold + blue + inline(text) + reset
	}
	if isRule(trimmed) {
		return dim + strings.Repeat("─", 40) + reset
	}
	if strings.HasPrefix(trimmed, "> ") {
		return dim + "│ " + reset + inline(trimmed[2:])
	}
	if indent, marker, content, ok := listItem(trimmed); ok {
		return indent + marker + " " + inline(content)
	}
	return inline(line)
}

// listItem splits a list line into its indentation, marker and content, so
// nested items keep their indentation and ordered items keep their number (the
// model emits both).
func listItem(line string) (indent, marker, content string, ok bool) {
	body := strings.TrimLeft(line, " \t")
	indent = line[:len(line)-len(body)]
	if len(body) >= 2 {
		switch body[0] {
		case '-', '*', '+':
			if body[1] == ' ' {
				return indent, "•", body[2:], true
			}
		}
	}
	if end := strings.IndexAny(body, ".)"); end > 0 && end <= 3 && end+1 < len(body) && body[end+1] == ' ' {
		if _, err := strconv.Atoi(body[:end]); err == nil {
			return indent, body[:end+1], body[end+2:], true
		}
	}
	return "", "", "", false
}

func heading(line string) (int, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func isRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != c {
			return false
		}
	}
	return true
}

// inline renders the common inline spans: code, bold, links, italic.
func inline(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				out.WriteString(cyan + s[i+1:i+1+end] + reset)
				i += end + 2
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				out.WriteString(bold + inline(s[i+2:i+2+end]) + reset)
				i += end + 4
				continue
			}
		case s[i] == '[':
			close := strings.IndexByte(s[i:], ']')
			if close >= 0 && i+close+1 < len(s) && s[i+close+1] == '(' {
				if urlEnd := strings.IndexByte(s[i+close+2:], ')'); urlEnd >= 0 {
					text := s[i+1 : i+close]
					url := s[i+close+2 : i+close+2+urlEnd]
					out.WriteString(underline + inline(text) + reset + dim + " (" + url + ")" + reset)
					i += close + 2 + urlEnd + 1
					continue
				}
			}
		case s[i] == '*':
			if end := strings.IndexByte(s[i+1:], '*'); end > 0 {
				text := s[i+1 : i+1+end]
				// Emphasis cannot start or end with a space, so arithmetic like
				// "2 * 3 * 4" stays literal instead of rendering as italic.
				if text == strings.TrimSpace(text) {
					out.WriteString(italic + inline(text) + reset)
					i += end + 2
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// markdownStream renders markdown line by line as text streams in.
type markdownStream struct {
	md    markdown
	buf   strings.Builder
	write func(string)
}

func newMarkdownStream(write func(string)) *markdownStream {
	return &markdownStream{write: write}
}

func (s *markdownStream) Write(delta string) {
	for _, r := range delta {
		if r == '\n' {
			s.write(s.md.render(s.buf.String()) + "\n")
			s.buf.Reset()
			continue
		}
		s.buf.WriteRune(r)
	}
}

func (s *markdownStream) Flush() {
	if s.buf.Len() > 0 {
		s.write(s.md.render(s.buf.String()))
		s.buf.Reset()
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

// formatToolcalls renders a tool-call count for the task result line.
func formatToolcalls(count int) string {
	if count == 1 {
		return "1 toolcall"
	}
	return fmt.Sprintf("%d toolcalls", count)
}

// spinnerFrames animates the bottom status line.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
	// calls maps an open parent tool call to its tool name, and running holds
	// the ids of its non-task calls in start order: the tool calls of one step
	// run in parallel, so the status line names the most recent call that is
	// still running rather than one field that any finishing call would clear.
	calls       map[string]string
	running     []string
	tasksByCall map[string]string
	tasks       map[string]*taskRecord
	order       []string
	answers     map[string]string
	streams     map[string]*markdownStream
	labeled     map[string]bool
}

func newChat(console *console, client *agent.Client, parent, model string) *chat {
	return &chat{
		console:     console,
		client:      client,
		parent:      parent,
		model:       model,
		busy:        map[string]bool{},
		calls:       map[string]string{},
		tasksByCall: map[string]string{},
		tasks:       map[string]*taskRecord{},
		answers:     map[string]string{},
		streams:     map[string]*markdownStream{},
		labeled:     map[string]bool{},
	}
}

func (c *chat) beginTurn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.labeled[c.parent] = false
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
	// Events without a session id (usage, retries) belong to the parent's turn,
	// as do the parent session's own events.
	if sessionID == "" || sessionID == c.parent {
		c.handleParent(event, sessionID)
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
		record := c.tasks[sessionID]
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
	parts := make([]string, 0, len(c.order))
	for _, childID := range c.order {
		if !c.busy[childID] {
			continue
		}
		label := "working"
		record := c.tasks[childID]
		if record != nil && record.lastTool != "" {
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

// flushStream writes any buffered (partial) assistant text so a following tool
// or task line does not appear before text that streamed earlier.
func (c *chat) flushStream(sessionID string) {
	if stream := c.streams[sessionID]; stream != nil {
		stream.Flush()
	}
}

func (c *chat) handleParent(event agent.Event, sessionID string) {
	switch e := event.(type) {
	case agent.ToolProgress:
		c.flushStream(sessionID)
		c.handleProgress(e)
	case agent.ToolCalled:
		c.flushStream(sessionID)
		c.openCall(e.CallID, e.Name)
		if e.Name == coretool.SkillName {
			// Skills are worth surfacing: unlike read/grep, a loaded skill
			// changes how the model behaves for the rest of the turn.
			c.console.write(fmt.Sprintf("%s[skill] %s%s\n", cyan, skillName(e.Input), reset))
		}
	case agent.ToolSuccess:
		c.flushStream(sessionID)
		c.finishTool(e.CallID, "")
	case agent.ToolFailed:
		c.flushStream(sessionID)
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
		if !c.labeled[sessionID] {
			c.console.write("Assistant: ")
			c.labeled[sessionID] = true
		}
		stream := c.streams[sessionID]
		if stream == nil {
			stream = newMarkdownStream(c.console.write)
			c.streams[sessionID] = stream
		}
		stream.Write(e.Delta)
	case agent.TextEnded:
		if stream := c.streams[sessionID]; stream != nil {
			stream.Flush()
		}
		c.console.write("\n")
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
	c.tasksByCall[event.CallID] = childID
	if _, seen := c.tasks[childID]; !seen {
		record := &taskRecord{
			index: len(c.order) + 1, callID: event.CallID, subagent: subagent,
			description: description, started: time.Now(),
		}
		c.tasks[childID] = record
		c.order = append(c.order, childID)
		c.console.write(fmt.Sprintf("\n%s┌ %s %s Task — %s%s\n",
			yellow, taskHandle(record), titlecase(subagent), description, reset))
	}
}

// finishTool closes a tool call. Non-task tools print nothing (the status line
// shows the current tool); a task result prints the result preview.
func (c *chat) finishTool(callID, failure string) {
	c.endCall(callID)
	childID, isTask := c.tasksByCall[callID]
	if !isTask {
		return
	}
	delete(c.tasksByCall, callID)

	record := c.tasks[childID]
	elapsed, tools := time.Duration(0), 0
	if record != nil {
		elapsed, tools = time.Since(record.started), record.tools
	}
	answer := ""
	if failure == "" {
		answer = c.subagentAnswer(childID)
		c.answers[childID] = answer
	}
	mark := "✓"
	if failure != "" {
		mark = "✗"
	}
	detail := elapsed.Round(100 * time.Millisecond).String()
	if tools > 0 {
		detail = formatToolcalls(tools) + " · " + detail
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
		if len(c.order) == 0 {
			c.console.write("(no tasks)\n")
			return
		}
		for _, childID := range c.order {
			record := c.tasks[childID]
			if record == nil {
				continue
			}
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
	for _, childID := range c.order {
		record := c.tasks[childID]
		if record == nil {
			continue
		}
		if arg != fmt.Sprint(record.index) && !strings.HasPrefix(record.description, arg) {
			continue
		}
		answer := c.answers[childID]
		if answer == "" {
			answer = c.subagentAnswer(childID)
		}
		c.console.write(fmt.Sprintf("\n%s┌ %s %s Task — %s%s\n",
			yellow, taskHandle(record), titlecase(record.subagent), record.description, reset))
		c.console.write(renderMarkdown(answer))
		c.console.write(fmt.Sprintf("%s└ %s%s\n", yellow, taskHandle(record), reset))
		return
	}
	c.console.write("task not found: " + arg + "\n")
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

// renderMarkdown renders a complete markdown block (used for /task output).
func renderMarkdown(text string) string {
	md := &markdown{}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = md.render(line)
	}
	return strings.Join(lines, "\n") + "\n"
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
