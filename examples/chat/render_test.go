package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
)

func TestInlineMarkdown(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**bold**", bold + "bold" + reset},
		{"`code`", cyan + "code" + reset},
		{"[text](http://x)", underline + "text" + reset + dim + " (http://x)" + reset},
		{"*it*", italic + "it" + reset},
		{"plain", "plain"},
	}
	for _, c := range cases {
		if got := inline(c.in); got != c.want {
			t.Errorf("inline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownStream(t *testing.T) {
	var out strings.Builder
	stream := newMarkdownStream(func(s string) { out.WriteString(s) })
	stream.Write("# Title\n")
	stream.Write("- item **bold**\n")
	stream.Write("```\n")
	stream.Write("raw *code*\n")
	stream.Write("```\n")
	stream.Flush()
	got := out.String()

	if !strings.Contains(got, bold+blue+"Title"+reset) {
		t.Errorf("heading not rendered: %q", got)
	}
	if !strings.Contains(got, "• item "+bold+"bold"+reset) {
		t.Errorf("list/bold not rendered: %q", got)
	}
	if !strings.Contains(got, cyan+"raw *code*"+reset) {
		t.Errorf("fenced code not rendered verbatim: %q", got)
	}
}

func TestErrorMessage(t *testing.T) {
	if got := errorMessage(map[string]any{"message": "boom"}); got != "boom" {
		t.Errorf("map message = %q", got)
	}
	if got := errorMessage("plain"); got != "plain" {
		t.Errorf("string = %q", got)
	}
	if got := errorMessage(nil); got != "failed" {
		t.Errorf("nil = %q", got)
	}
}

func TestSummarize(t *testing.T) {
	if got := summarize("first line\nsecond", ""); got != "Summary: first line" {
		t.Errorf("got %q", got)
	}
	if got := summarize("", ""); got != "Summary: (no output)" {
		t.Errorf("got %q", got)
	}
	if got := summarize("", "boom"); got != "Summary: failed: boom" {
		t.Errorf("got %q", got)
	}
	if got := summarize(strings.Repeat("字", 100), ""); !strings.HasSuffix(got, "…") {
		t.Errorf("long answer not truncated: %q", got)
	}
}

func TestTaskHandle(t *testing.T) {
	if got := taskHandle(&taskRecord{index: 3}); got != "#3" {
		t.Errorf("got %q", got)
	}
	if got := taskHandle(nil); got != "#?" {
		t.Errorf("nil handle = %q", got)
	}
}

func taskProgress(callID, childID string) agent.ToolProgress {
	return taskProgressFrom("ses_parent", callID, childID)
}

// taskProgressFrom is taskProgress as published by another session, i.e. a task
// started by a subagent rather than by the chat session itself.
func taskProgressFrom(publisher, callID, childID string) agent.ToolProgress {
	return agent.ToolProgress{
		SessionID: publisher,
		CallID:    callID,
		Structured: map[string]any{
			"sessionId":       childID,
			"parentSessionId": publisher,
			"subagentType":    "explore",
			"description":     "explore core",
		},
	}
}

// TestTextFlushesBeforeTaskLine guards the ordering fix: buffered assistant
// text must be written before a task's start line, not after it.
func TestTextFlushesBeforeTaskLine(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(agent.TextDelta{SessionID: "ses_parent", Delta: "launching agents"})
	c.handle(taskProgress("call_1", "ses_child"))

	got := out.String()
	textAt := strings.Index(got, "launching agents")
	taskAt := strings.Index(got, "┌ #1")
	if textAt < 0 || taskAt < 0 {
		t.Fatalf("missing output: %q", got)
	}
	if textAt > taskAt {
		t.Fatalf("text appeared after the task line:\n%q", got)
	}
}

// TestChildToolCallsAreNotLogged guards that per-tool-call lines are gone: the
// bottom status line (see TestUpdateStatusShowsBusyTasks) is the only trace.
func TestChildToolCallsAreNotLogged(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(taskProgress("call_1", "ses_child"))
	out.Reset()

	for i := 0; i < 25; i++ {
		c.handle(agent.ToolCalled{SessionID: "ses_child", CallID: fmt.Sprintf("c%d", i), Name: "read"})
	}
	// Failures are not printed either (bad tool arguments are common).
	c.handle(agent.ToolFailed{SessionID: "ses_child", CallID: "c0", Error: "invalid input"})
	if out.String() != "" {
		t.Fatalf("child tool calls should not be logged: %q", out.String())
	}
	// The current tool is still tracked for the status line.
	if record := c.tasks.byID["ses_child"]; record.lastTool != "read" || record.tools != 25 {
		t.Fatalf("task not tracked: %+v", record)
	}
}

func TestInlineCodeInContexts(t *testing.T) {
	code := cyan + "config/watcher.go" + reset
	cases := []struct{ name, in, want string }{
		{"plain", "see `config/watcher.go` now", "see " + code + " now"},
		{"list", "- `config/watcher.go`", "• " + code},
		{"heading", "# see `config/watcher.go`", bold + blue + "see " + code + reset},
		{"bold", "**see `config/watcher.go`**", bold + "see " + code + reset},
	}
	md := &markdown{}
	for _, c := range cases {
		if got := md.render(c.in); got != c.want {
			t.Errorf("%s: render(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestUpdateStatusShowsBusyTasks guards the bottom status line: it names every
// busy subagent with its current tool and clears once they go idle.
func TestUpdateStatusShowsBusyTasks(t *testing.T) {
	c := newChat(&console{out: &strings.Builder{}}, nil, "ses_parent", "")
	c.handle(taskProgress("call_1", "ses_child"))
	c.handle(agent.SessionStatus{SessionID: "ses_child", Status: "busy"})
	c.tasks.byID["ses_child"].lastTool = "read"

	c.updateStatus(0)
	got := c.console.status
	if !strings.Contains(got, "#1") || !strings.Contains(got, "read") || !strings.Contains(got, spinnerFrames[0]) {
		t.Fatalf("status = %q", got)
	}

	c.handle(agent.SessionStatus{SessionID: "ses_child", Status: "idle"})
	c.updateStatus(0)
	if c.console.status != "" {
		t.Fatalf("status not cleared after idle: %q", c.console.status)
	}
}

// TestUpdateStatusParentWorking guards the lone-parent spinner.
func TestUpdateStatusParentWorking(t *testing.T) {
	c := newChat(&console{out: &strings.Builder{}}, nil, "ses_parent", "")
	c.handle(agent.SessionStatus{SessionID: "ses_parent", Status: "busy"})
	c.updateStatus(0)
	if !strings.Contains(c.console.status, "working") {
		t.Fatalf("status = %q", c.console.status)
	}
	c.clearStatus()
	if c.console.status != "" {
		t.Fatalf("status not cleared: %q", c.console.status)
	}
}

// TestParentToolCallsAreNotLogged guards that parent (non-task) tool calls and
// failures are silent; only the status line reflects the current tool.
func TestParentToolCallsAreNotLogged(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(agent.ToolCalled{SessionID: "ses_parent", CallID: "c1", Name: "webfetch"})
	c.handle(agent.ToolFailed{SessionID: "ses_parent", CallID: "c1", Error: map[string]any{"message": "invalid input"}})
	if out.String() != "" {
		t.Fatalf("parent tool calls should not be logged: %q", out.String())
	}
	if c.currentTool() != "" {
		t.Fatalf("current tool not cleared: %q", c.currentTool())
	}
}

// TestUpdateStatusShowsParentTool guards that the status line names the tool the
// parent session is currently running.
func TestUpdateStatusShowsParentTool(t *testing.T) {
	c := newChat(&console{out: &strings.Builder{}}, nil, "ses_parent", "")
	c.handle(agent.ToolCalled{SessionID: "ses_parent", CallID: "c1", Name: "webfetch"})
	c.handle(agent.SessionStatus{SessionID: "ses_parent", Status: "busy"})
	c.updateStatus(0)
	if !strings.Contains(c.console.status, "webfetch") {
		t.Fatalf("status = %q", c.console.status)
	}
}

// TestSkillCallIsLogged guards that a skill load is visible: unlike ordinary
// tool calls, it changes how the model behaves for the rest of the turn.
func TestSkillCallIsLogged(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(agent.ToolCalled{
		SessionID: "ses_parent", CallID: "c1", Name: "skill",
		Input: map[string]any{"name": "code-review"},
	})

	if got := out.String(); !strings.Contains(got, "[skill] code-review") {
		t.Fatalf("out = %q, want the skill line", got)
	}
}

// TestOrdinaryToolCallIsNotLogged guards that read/grep-style calls stay quiet.
func TestOrdinaryToolCallIsNotLogged(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(agent.ToolCalled{
		SessionID: "ses_parent", CallID: "c1", Name: "read",
		Input: map[string]any{"path": "x"},
	})

	if got := out.String(); got != "" {
		t.Fatalf("out = %q, want nothing", got)
	}
}

func TestSkillName(t *testing.T) {
	cases := []struct {
		input any
		want  string
	}{
		{map[string]any{"name": "code-review"}, "code-review"},
		{"release-notes", "release-notes"},
		{map[string]any{"other": 1}, "?"},
		{nil, "?"},
	}
	for _, tc := range cases {
		if got := skillName(tc.input); got != tc.want {
			t.Fatalf("skillName(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestNestedSubagentIsTrackedButNotRendered guards the session routing: a task
// started by a subagent gets its own record, and neither its text nor its tool
// calls may be attributed to the chat session.
func TestNestedSubagentIsTrackedButNotRendered(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(taskProgress("call_1", "ses_child"))
	c.handle(taskProgressFrom("ses_child", "call_2", "ses_grandchild"))
	out.Reset()

	c.handle(agent.TextDelta{SessionID: "ses_grandchild", Delta: "grandchild text"})
	c.handle(agent.ToolCalled{SessionID: "ses_grandchild", CallID: "c1", Name: "read"})

	if got := out.String(); got != "" {
		t.Fatalf("nested subagent output leaked into the transcript: %q", got)
	}
	if c.tasks.byID["ses_grandchild"] == nil {
		t.Fatal("nested task was not registered")
	}
	if record := c.tasks.byID["ses_grandchild"]; record.tools != 1 || record.lastTool != "read" {
		t.Fatalf("nested task not tracked: %+v", record)
	}
	if record := c.tasks.byID["ses_child"]; record.tools != 0 {
		t.Fatalf("nested calls counted against the parent task: %+v", record)
	}
}

// TestChildTextAfterItsTaskStaysWithTheChild guards that a subagent's session is
// not mistaken for the chat session once its task has finished: its text belongs
// to /task, and its status must not move the chat's spinner.
func TestChildTextAfterItsTaskStaysWithTheChild(t *testing.T) {
	var out strings.Builder
	c := newChat(&console{out: &out}, nil, "ses_parent", "")
	c.handle(taskProgress("call_1", "ses_child"))
	c.handle(agent.ToolFailed{SessionID: "ses_parent", CallID: "call_1", Error: "boom"})
	out.Reset()

	c.handle(agent.TextDelta{SessionID: "ses_child", Delta: "late text"})
	c.handle(agent.SessionStatus{SessionID: "ses_child", Status: "busy"})

	if got := out.String(); got != "" {
		t.Fatalf("out = %q, want the child's text kept out of the transcript", got)
	}
	if c.parentBusy {
		t.Fatal("a subagent session's status was applied to the chat session")
	}
}

// TestParallelToolCallsKeepCurrentTool guards the status line while several tool
// calls of one step run at once: a finishing call must not clear the label of a
// call that is still running.
func TestParallelToolCallsKeepCurrentTool(t *testing.T) {
	c := newChat(&console{out: &strings.Builder{}}, nil, "ses_parent", "")
	c.handle(agent.ToolCalled{SessionID: "ses_parent", CallID: "c1", Name: "read"})
	c.handle(agent.ToolCalled{SessionID: "ses_parent", CallID: "c2", Name: "grep"})
	if got := c.currentTool(); got != "grep" {
		t.Fatalf("currentTool = %q, want grep", got)
	}

	c.handle(agent.ToolSuccess{SessionID: "ses_parent", CallID: "c2"})
	if got := c.currentTool(); got != "read" {
		t.Fatalf("currentTool = %q, want the still running read", got)
	}

	c.handle(agent.ToolSuccess{SessionID: "ses_parent", CallID: "c1"})
	if got := c.currentTool(); got != "" {
		t.Fatalf("currentTool = %q, want empty", got)
	}
}

// TestTaskCallsAreNotTheCurrentTool guards that delegating shows the task, not
// the task tool.
func TestTaskCallsAreNotTheCurrentTool(t *testing.T) {
	c := newChat(&console{out: &strings.Builder{}}, nil, "ses_parent", "")
	c.handle(taskProgress("call_1", "ses_child"))
	c.handle(agent.ToolCalled{SessionID: "ses_parent", CallID: "call_1", Name: agent.TaskName})
	if got := c.currentTool(); got != "" {
		t.Fatalf("currentTool = %q, want no tool for a task call", got)
	}
}

// TestRenderLists guards list rendering: nested items keep their indentation and
// ordered items keep their number.
func TestRenderLists(t *testing.T) {
	cases := []struct{ in, want string }{
		{"- item", "• item"},
		{"+ item", "• item"},
		{"  - nested", "  • nested"},
		{"1. first", "1. first"},
		{"2) second", "2) second"},
		{"- **bold**", "• " + bold + "bold" + reset},
		{"---", dim + strings.Repeat("─", 40) + reset},
		{"no list", "no list"},
	}
	md := &markdown{}
	for _, c := range cases {
		if got := md.render(c.in); got != c.want {
			t.Errorf("render(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAsterisksAreNotEmphasis guards that arithmetic and other bare asterisks
// stay literal: emphasis cannot begin or end with a space.
func TestAsterisksAreNotEmphasis(t *testing.T) {
	if got := inline("2 * 3 * 4"); got != "2 * 3 * 4" {
		t.Errorf("inline = %q, want the literal asterisks", got)
	}
	if got := inline("a *b* c"); got != "a "+italic+"b"+reset+" c" {
		t.Errorf("inline = %q, want the emphasis", got)
	}
}

// TestUsageLineOmitsUnpricedCost guards the status line for a model this demo
// cannot price: the tokens stand alone instead of being reported as ¥0.
func TestUsageLineOmitsUnpricedCost(t *testing.T) {
	c := newChat(&console{out: &strings.Builder{}}, nil, "ses_parent", "")
	c.usage.output = 1500
	if got := c.usageLine(); got != "1.5k tok" {
		t.Fatalf("unpriced usageLine = %q", got)
	}
	c.usage.cost, c.usage.priced = 0.5, true
	if got := c.usageLine(); got != "1.5k tok · ¥0.5000" {
		t.Fatalf("priced usageLine = %q", got)
	}
}
