package server

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	a2acommon "github.com/jiaweisky/go-agent-sdk/a2a"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// fakeSession is an in-memory Session that records whether it was closed and
// fires OnTurnEnd callbacks when a turn's iterator returns.
type fakeSession struct {
	id         string
	closed     bool
	events     []Event
	lastPrompt string
	lastFiles  []File
	steered    []string
	onEnd      []func()
}

func (s *fakeSession) ID() string { return s.id }

func (s *fakeSession) OnTurnEnd(_ string, fn func()) { s.onEnd = append(s.onEnd, fn) }

func (s *fakeSession) Send(ctx context.Context, turnID string, req SendRequest) iter.Seq2[Event, error] {
	s.lastPrompt = req.Prompt
	s.lastFiles = req.Files
	return s.run(s.events)
}

func (s *fakeSession) Steer(prompt string) error {
	s.steered = append(s.steered, prompt)
	return nil
}

func (s *fakeSession) Resume(ctx context.Context, turnID string) iter.Seq2[Event, error] {
	return s.run([]Event{{Kind: EventText, Text: "resumed"}, {Kind: EventIdle}})
}

// run yields events and fires the pending OnTurnEnd callbacks when the iterator
// returns, matching the adapter's forwarder goroutine.
func (s *fakeSession) run(events []Event) iter.Seq2[Event, error] {
	callbacks := s.onEnd
	s.onEnd = nil
	return func(yield func(Event, error) bool) {
		defer func() {
			for _, fn := range callbacks {
				fn()
			}
		}()
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

func (s *fakeSession) ReplyPermission(string, string) error { return nil }
func (s *fakeSession) ReplyQuestion(string, []Answer) error { return nil }
func (s *fakeSession) RejectQuestion(string) error          { return nil }
func (s *fakeSession) Abort()                               {}
func (s *fakeSession) Close() error                         { s.closed = true; return nil }

type fakeFactory struct {
	session *fakeSession
	last    SessionRequest
}

func (f *fakeFactory) CreateSession(_ context.Context, req SessionRequest) (Session, error) {
	f.last = req
	return f.session, nil
}

func (f *fakeFactory) ResumeSession(_ context.Context, _ string, workspace Workspace) (Session, error) {
	f.last = SessionRequest{Workspace: workspace}
	return f.session, nil
}

func newExecCtx(taskID a2a.TaskID, contextID string) *a2asrv.ExecutorContext {
	return &a2asrv.ExecutorContext{
		TaskID:    taskID,
		ContextID: contextID,
		Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi")),
	}
}

// collect drains an executor stream and returns the states observed.
func collect(t *testing.T, exec *executor, execCtx *a2asrv.ExecutorContext) []a2a.TaskState {
	t.Helper()
	var states []a2a.TaskState
	for event, err := range exec.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if status, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
			states = append(states, status.Status.State)
		}
	}
	return states
}

func TestExecutorReleasesSessionAfterCompletion(t *testing.T) {
	session := &fakeSession{id: "ses_done", events: []Event{{Kind: EventText, Text: "ok"}, {Kind: EventIdle}}}
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	collect(t, exec, newExecCtx(a2a.NewTaskID(), a2a.NewContextID()))

	if !session.closed {
		t.Fatal("session was not released after the turn completed")
	}
	if _, ok := exec.sessions[session.id]; ok {
		t.Fatal("session stayed in the cache after completion")
	}
}

func TestExecutorKeepsParkedSessionUntilResume(t *testing.T) {
	session := &fakeSession{
		id: "ses_park",
		events: []Event{
			{Kind: EventText, Text: "partial"},
			{Kind: EventAwaitInput, RequestID: "que_1", IsQuestion: true},
		},
	}
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	taskID := a2a.NewTaskID()
	contextID := a2a.NewContextID()

	// First leg: the turn parks at input-required and must not be released.
	var sawSeal bool
	for event, err := range exec.Execute(context.Background(), newExecCtx(taskID, contextID)) {
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if artifact, ok := event.(*a2a.TaskArtifactUpdateEvent); ok && artifact.LastChunk {
			sawSeal = true
		}
	}
	if !sawSeal {
		t.Fatal("parked turn did not seal its open artifact")
	}
	if session.closed {
		t.Fatal("parked session was released before the reply arrived")
	}

	// Second leg: the reply resumes and completes the same turn.
	stored := taskstore.StoredTask{Task: &a2a.Task{
		ID:        taskID,
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateInputRequired},
		Metadata:  map[string]any{metaKeySession: session.id},
	}}
	resumeCtx := &a2asrv.ExecutorContext{
		TaskID:     taskID,
		ContextID:  contextID,
		StoredTask: stored.Task,
		Message:    a2a.NewMessageForTask(a2a.MessageRoleUser, stored.Task, a2a.NewTextPart("blue")),
	}
	states := collect(t, exec, resumeCtx)
	if len(states) == 0 || states[len(states)-1] != a2a.TaskStateCompleted {
		t.Fatalf("final states = %v", states)
	}
	if !session.closed {
		t.Fatal("session was not released after the resumed turn completed")
	}
}

func TestExecutorMapsFileDiffAndUsage(t *testing.T) {
	session := &fakeSession{id: "ses_meta", events: []Event{
		{Kind: EventToolResult, CallID: "c1", ToolName: "edit", Result: "ok", Structured: map[string]any{
			"files": []any{map[string]any{"file": "a.go", "additions": 3, "deletions": 1, "status": "modified"}},
		}},
		{Kind: EventUsage, Usage: map[string]map[string]any{"openai": {"input": 10, "output": 2}}},
		{Kind: EventText, Text: "done"},
		{Kind: EventIdle},
	}}
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	fileDiffs, usageOnSeal := 0, false
	for event, err := range exec.Execute(context.Background(), newExecCtx(a2a.NewTaskID(), a2a.NewContextID())) {
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		artifact, ok := event.(*a2a.TaskArtifactUpdateEvent)
		if !ok {
			continue
		}
		for _, part := range artifact.Artifact.Parts {
			kind, _ := part.Metadata[a2acommon.ToSDKObjectMetaKey(a2acommon.MetaType)].(string)
			if kind == a2acommon.MetaFileDiff {
				fileDiffs++
			}
		}
		if artifact.LastChunk {
			if _, ok := artifact.Artifact.Metadata[a2acommon.ToA2AMetaKey(a2acommon.MetaUsageMetadata)]; ok {
				usageOnSeal = true
			}
		}
	}
	if fileDiffs != 1 {
		t.Fatalf("file_diff parts = %d, want 1", fileDiffs)
	}
	if !usageOnSeal {
		t.Fatal("usage metadata missing from the sealed artifact")
	}
}

func TestExecutorRebuildsAfterRestart(t *testing.T) {
	session := &fakeSession{id: "ses_restart", events: []Event{{Kind: EventText, Text: "rebuilt"}, {Kind: EventIdle}}}
	// A fresh executor has no in-process pending: this simulates a restart.
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	taskID := a2a.NewTaskID()
	contextID := a2a.NewContextID()
	original := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("original request"))
	question := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("which color?"))
	stored := &a2a.Task{
		ID:        taskID,
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question},
		History:   []*a2a.Message{original},
		Metadata:  map[string]any{metaKeySession: session.id, metaKeyAgent: "build"},
	}
	reply := a2a.NewMessageForTask(a2a.MessageRoleUser, stored, a2a.NewTextPart("blue"))
	execCtx := &a2asrv.ExecutorContext{TaskID: taskID, ContextID: contextID, StoredTask: stored, Message: reply}

	states := collect(t, exec, execCtx)
	if !strings.Contains(session.lastPrompt, "original request") || !strings.Contains(session.lastPrompt, "blue") {
		t.Fatalf("rebuild prompt = %q", session.lastPrompt)
	}
	if len(states) == 0 || states[len(states)-1] != a2a.TaskStateCompleted {
		t.Fatalf("final states = %v", states)
	}
}

func TestExecutorResolvesWorkspace(t *testing.T) {
	session := &fakeSession{id: "ses_ws", events: []Event{{Kind: EventIdle}}}
	factory := &fakeFactory{session: session}
	resolved := Workspace{Permissions: schema.PermissionRuleset{{Action: "bash", Resource: "*", Effect: schema.PermissionEffectDeny}}}

	var got WorkspaceRequest
	exec := newExecutor(executorConfig{
		factory: factory, agentID: "build", store: taskstore.NewInMemory(nil),
		workspaces: WorkspaceResolverFunc(func(_ context.Context, req WorkspaceRequest) (Workspace, error) {
			got = req
			return resolved, nil
		}),
	})

	execCtx := newExecCtx(a2a.NewTaskID(), "ctx-ws")
	execCtx.User = &a2asrv.User{Name: "alice"}
	collect(t, exec, execCtx)

	if got.User != "alice" || got.AgentID != "build" || got.ContextID != "ctx-ws" {
		t.Fatalf("workspace request = %+v", got)
	}
	if len(factory.last.Workspace.Permissions) != 1 {
		t.Fatalf("factory workspace = %+v", factory.last.Workspace)
	}
}

func TestExecutorExternalizesArtifact(t *testing.T) {
	session := &fakeSession{id: "ses_ext", events: []Event{{Kind: EventText, Text: "original"}, {Kind: EventIdle}}}
	exec := newExecutor(executorConfig{
		factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil),
		artifacts: func(_ context.Context, _ string, artifact any) (any, error) {
			original := artifact.(*a2a.Artifact)
			return &a2a.Artifact{ID: original.ID, Parts: a2a.ContentParts{a2a.NewTextPart("externalized")}}, nil
		},
	})

	found := false
	for event, err := range exec.Execute(context.Background(), newExecCtx(a2a.NewTaskID(), a2a.NewContextID())) {
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if artifact, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			for _, part := range artifact.Artifact.Parts {
				if part.Text() == "externalized" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("artifact was not externalized")
	}
}

func TestExecutorPassesInboundFiles(t *testing.T) {
	raw := a2a.NewRawPart([]byte("hello bytes"))
	raw.Filename = "note.txt"
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("see attached"), raw)

	session := &fakeSession{id: "ses_files", events: []Event{{Kind: EventIdle}}}
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	execCtx := newExecCtx(a2a.NewTaskID(), a2a.NewContextID())
	execCtx.Message = message
	collect(t, exec, execCtx)

	if len(session.lastFiles) != 1 {
		t.Fatalf("files = %d, want 1", len(session.lastFiles))
	}
	if string(session.lastFiles[0].Data) != "hello bytes" || session.lastFiles[0].Name != "note.txt" {
		t.Fatalf("file = %+v", session.lastFiles[0])
	}
}

func TestExecutorSteersWorkingTask(t *testing.T) {
	session := &fakeSession{id: "ses_steer"}
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	taskID := a2a.NewTaskID()
	stored := &a2a.Task{
		ID:        taskID,
		ContextID: "ctx",
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
		Metadata:  map[string]any{metaKeySession: "ses_steer"},
	}
	execCtx := &a2asrv.ExecutorContext{
		TaskID:     taskID,
		ContextID:  "ctx",
		StoredTask: stored,
		Message:    a2a.NewMessageForTask(a2a.MessageRoleUser, stored, a2a.NewTextPart("more")),
	}
	collect(t, exec, execCtx)

	if len(session.steered) != 1 || session.steered[0] != "more" {
		t.Fatalf("steered = %v, want [more]", session.steered)
	}
	if session.lastPrompt != "" {
		t.Fatalf("working task should steer, not Send (prompt = %q)", session.lastPrompt)
	}
}

func TestExecutorForwardsQuestions(t *testing.T) {
	session := &fakeSession{id: "ses_questions", events: []Event{{
		Kind:       EventAwaitInput,
		RequestID:  "que_1",
		IsQuestion: true,
		Questions:  []schema.QuestionInfo{{Question: "Pick a color", Header: "color", Options: []schema.QuestionOption{{Label: "blue", Description: "cool"}}}},
	}}}
	exec := newExecutor(executorConfig{factory: &fakeFactory{session: session}, agentID: "build", store: taskstore.NewInMemory(nil)})

	var questions []schema.QuestionInfo
	for event, err := range exec.Execute(context.Background(), newExecCtx(a2a.NewTaskID(), a2a.NewContextID())) {
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		status, ok := event.(*a2a.TaskStatusUpdateEvent)
		if !ok || status.Status.State != a2a.TaskStateInputRequired {
			continue
		}
		for _, part := range status.Status.Message.Parts {
			data, _ := part.Data().(map[string]any)
			args, _ := data["args"].(map[string]any)
			raw, ok := args["questions"]
			if !ok {
				continue
			}
			encoded, _ := json.Marshal(raw)
			_ = json.Unmarshal(encoded, &questions)
		}
	}
	if len(questions) != 1 || questions[0].Question != "Pick a color" || len(questions[0].Options) != 1 {
		t.Fatalf("forwarded questions = %+v", questions)
	}
}
