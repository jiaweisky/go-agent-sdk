package agent

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/go-agent-sdk/a2a/client"
	"github.com/user/go-agent-sdk/a2a/server"
	"github.com/user/go-agent-sdk/core/session/runner"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

// startA2AServer exposes the client's "build" agent over A2A and returns its URL.
func startA2AServer(t *testing.T, client *Client) string {
	t.Helper()
	if err := client.RegisterAgent("build", AgentConfig{
		Expose: &schema.AgentExpose{Name: "Remote"},
	}); err != nil {
		t.Fatalf("expose agent: %v", err)
	}
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	srv, err := server.New(server.Options{
		Factory: NewA2AFactory(client),
		Agents:  client.Agents().All(),
		Turns:   client.TurnStore(),
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("new a2a server: %v", err)
	}
	handler = srv.Handler()
	return ts.URL
}

// backendStubLLM is an unused local model for the delegating client; the remote
// agent overrides it.
type backendStubLLM struct{}

func (backendStubLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}
func (backendStubLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}
func (backendStubLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return func(yield func(llmschema.LLMEvent, error) bool) {}, nil
}

func TestRemoteA2AAgentAsMainAgent(t *testing.T) {
	// Remote host: a normal client with a stub model, exposed over A2A.
	remoteHost := newTestClient(t)
	endpoint := startA2AServer(t, remoteHost)

	// Caller: a client whose "remote" agent delegates to the A2A endpoint.
	caller, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: backendStubLLM{},
	})
	if err != nil {
		t.Fatalf("new caller: %v", err)
	}
	t.Cleanup(func() { _ = caller.Close() })

	err = caller.RegisterAgent("remote", AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: endpoint},
		},
	})
	if err != nil {
		t.Fatalf("register remote agent: %v", err)
	}

	session, err := caller.CreateSession(context.Background(), &SessionConfig{Agent: "remote"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var text strings.Builder
	for event, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case TextDelta:
			text.WriteString(e.Delta)
		case TextEnded:
			text.WriteString(e.Text)
		}
	}
	if !strings.Contains(text.String(), "hello world") {
		t.Fatalf("remote output = %q", text.String())
	}
}

// remoteTaskParentLLM emits a `task` tool call delegating to the remote agent,
// then reports whether the child's output reached the parent context.
type remoteTaskParentLLM struct{ calls *int }

func (f remoteTaskParentLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}
func (f remoteTaskParentLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}
func (f remoteTaskParentLLM) Stream(_ context.Context, req *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	*f.calls++
	if *f.calls == 1 {
		return eventSeq(toolCallEvents("task", map[string]any{
			"description":   "delegate",
			"prompt":        "do it",
			"subagent_type": "remote",
		})), nil
	}
	encoded, err := json.Marshal(req.Messages)
	if err == nil && strings.Contains(string(encoded), "hello world") {
		return eventSeq(textEvents("parent saw child")), nil
	}
	return eventSeq(textEvents("parent missed child")), nil
}

func TestRemoteA2AAgentAsSubagent(t *testing.T) {
	remoteHost := newTestClient(t)
	endpoint := startA2AServer(t, remoteHost)

	calls := 0
	caller, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: remoteTaskParentLLM{calls: &calls},
	})
	if err != nil {
		t.Fatalf("new caller: %v", err)
	}
	t.Cleanup(func() { _ = caller.Close() })
	if err := caller.RegisterAgent("build", AgentConfig{System: "parent"}); err != nil {
		t.Fatalf("register build: %v", err)
	}
	if err := caller.RegisterAgent("remote", AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: endpoint},
		},
	}); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	session, err := caller.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var text strings.Builder
	for event, err := range session.Send(context.Background(), MessageOptions{Prompt: "go"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case TextDelta:
			text.WriteString(e.Delta)
		case TextEnded:
			text.WriteString(e.Text)
		}
	}
	if !strings.Contains(text.String(), "parent saw child") {
		t.Fatalf("parent output = %q", text.String())
	}
}

func TestRemoteA2AAgentReusesRemoteSessionAcrossTurns(t *testing.T) {
	remoteHost := newTestClient(t)
	endpoint := startA2AServer(t, remoteHost)

	caller, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: backendStubLLM{},
	})
	if err != nil {
		t.Fatalf("new caller: %v", err)
	}
	t.Cleanup(func() { _ = caller.Close() })
	if err := caller.RegisterAgent("remote", AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: endpoint},
		},
	}); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	session, err := caller.CreateSession(context.Background(), &SessionConfig{Agent: "remote"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	for turn := 0; turn < 2; turn++ {
		for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
			if err != nil {
				t.Fatalf("turn %d: %v", turn, err)
			}
		}
	}

	// Both turns share one A2A context, so the remote must mirror them into a
	// single session rather than creating one per turn.
	sessions, err := remoteHost.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("remote sessions = %d, want 1", len(sessions))
	}
}

func TestA2AStreamMapsRemoteToolsAsProviderExecuted(t *testing.T) {
	var events []llmschema.LLMEvent
	stream := &a2aStream{yield: func(event llmschema.LLMEvent, err error) bool {
		events = append(events, event)
		return true
	}}
	stream.stepStart()
	stream.forward(client.Event{Kind: client.EventToolCall, CallID: "c1", ToolName: "edit", Input: map[string]any{"file": "a.go"}})
	stream.forward(client.Event{Kind: client.EventToolResult, CallID: "c1", ToolName: "edit", Result: map[string]any{"ok": true}})
	stream.finish()

	var call, result *llmschema.LLMEvent
	for i := range events {
		switch events[i].Type {
		case llmschema.EventToolCall:
			call = &events[i]
		case llmschema.EventToolResult:
			result = &events[i]
		}
	}
	if call == nil || call.ProviderExecuted == nil || !*call.ProviderExecuted {
		t.Fatalf("tool call not marked provider-executed: %+v", call)
	}
	if result == nil || result.ProviderExecuted == nil || !*result.ProviderExecuted {
		t.Fatalf("tool result not marked provider-executed: %+v", result)
	}
}

func TestA2AStreamSkipsOrphanToolResult(t *testing.T) {
	var events []llmschema.LLMEvent
	stream := &a2aStream{yield: func(event llmschema.LLMEvent, err error) bool {
		events = append(events, event)
		return true
	}}
	stream.forward(client.Event{Kind: client.EventToolResult, CallID: "missing", ToolName: "edit"})
	for _, event := range events {
		if event.Type == llmschema.EventToolResult {
			t.Fatal("orphan tool result was emitted")
		}
	}
}

func TestRemoteA2AAgentSurfacesRemoteToolTrace(t *testing.T) {
	calls := 0
	remoteHost, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "remote done"},
		Tools: []Tool{DefineTool("echo", "echo", func(struct{}, ToolContext) (string, error) {
			return "ok", nil
		})},
	})
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	t.Cleanup(func() { _ = remoteHost.Close() })
	endpoint := startA2AServer(t, remoteHost)

	caller, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: backendStubLLM{},
	})
	if err != nil {
		t.Fatalf("caller: %v", err)
	}
	t.Cleanup(func() { _ = caller.Close() })
	if err := caller.RegisterAgent("remote", AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: endpoint},
		},
	}); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	session, err := caller.CreateSession(context.Background(), &SessionConfig{Agent: "remote"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var called, succeeded bool
	for event, err := range session.Send(context.Background(), MessageOptions{Prompt: "go"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case ToolCalled:
			called = called || e.Name == "echo"
		case ToolSuccess:
			succeeded = succeeded || e.CallID != ""
		}
	}
	if !called {
		t.Fatal("remote tool call was not surfaced locally")
	}
	if !succeeded {
		t.Fatal("remote tool result was not surfaced locally")
	}
}

func TestRemoteA2AAgentHITLRoundTrip(t *testing.T) {
	remoteCalls := 0
	remoteHost, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: questionLLM{calls: &remoteCalls, reply: "remote answered"},
	})
	if err != nil {
		t.Fatalf("remote host: %v", err)
	}
	t.Cleanup(func() { _ = remoteHost.Close() })
	endpoint := startA2AServer(t, remoteHost)

	caller, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: ApproveAll,
		LLM: backendStubLLM{},
	})
	if err != nil {
		t.Fatalf("caller: %v", err)
	}
	t.Cleanup(func() { _ = caller.Close() })
	caller.HoldInteractions(true)
	if err := caller.RegisterAgent("remote", AgentConfig{
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A:  &schema.AgentA2ABackend{Endpoint: endpoint},
		},
	}); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	session, err := caller.CreateSession(context.Background(), &SessionConfig{Agent: "remote"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var (
		text     strings.Builder
		answered bool
	)
	for event, err := range session.Send(context.Background(), MessageOptions{Prompt: "go"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case QuestionAsked:
			answered = true
			if err := caller.Question().Reply(e.Request.ID, []schema.QuestionAnswer{{"blue"}}); err != nil {
				t.Fatalf("reply: %v", err)
			}
		case TextDelta:
			text.WriteString(e.Delta)
		case TextEnded:
			text.WriteString(e.Text)
		}
	}
	if !answered {
		t.Fatal("remote input-required did not surface as a local question")
	}
	if !strings.Contains(text.String(), "remote answered") {
		t.Fatalf("final text = %q", text.String())
	}
}

// fakeRemote is a client.Client whose Send blocks until the context is
// canceled, so the abort path can be exercised.
type fakeRemote struct {
	taskID   string
	started  chan struct{}
	canceled chan string
}

func (f *fakeRemote) Card() client.Card { return client.Card{} }

func (f *fakeRemote) Send(ctx context.Context, _ client.Request) iter.Seq2[client.Event, error] {
	return func(yield func(client.Event, error) bool) {
		// A non-terminal event so the backend considers the task active.
		if !yield(client.Event{Kind: client.EventStatus, TaskID: f.taskID, ContextID: "ctx"}, nil) {
			return
		}
		close(f.started)
		<-ctx.Done()
		yield(client.Event{}, ctx.Err())
	}
}

func (f *fakeRemote) Cancel(_ context.Context, taskID string) error {
	f.canceled <- taskID
	return nil
}

func (f *fakeRemote) Close() error { return nil }

func TestA2ABackendCancelsRemoteOnAbort(t *testing.T) {
	remote := &fakeRemote{taskID: "task-1", started: make(chan struct{}), canceled: make(chan string, 1)}
	pool := newA2ABackendPool(nil)
	pool.newClient = func(context.Context, string, map[string]string) (client.Client, error) {
		return remote, nil
	}
	backend := pool.forAgent(&schema.AgentInfo{Backend: &schema.AgentBackend{
		Type: schema.AgentBackendA2A,
		A2A:  &schema.AgentA2ABackend{Endpoint: "http://remote"},
	}})
	if backend == nil {
		t.Fatal("forAgent returned no backend")
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := backend.Stream(ctx, &llmschema.LLMRequest{
		Metadata: map[string]any{runner.RequestMetaSessionID: "ses_1"},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	go func() {
		<-remote.started
		cancel()
	}()
	for range stream {
		// Drain; cancellation surfaces as a terminal error.
	}

	select {
	case id := <-remote.canceled:
		if id != "task-1" {
			t.Fatalf("canceled task = %q, want task-1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remote task was not canceled on abort")
	}
}

func TestRemoteTaskPersistenceRoundTrip(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	now := time.Now().UnixMilli()
	turnID := schema.NewTurnID()
	if err := client.TurnStore().Create(schema.TurnInfo{
		ID: turnID, SessionID: schema.SessionID(session.ID()), State: schema.TurnStateRunning,
		TimeCreated: now, TimeUpdated: now,
	}); err != nil {
		t.Fatalf("create turn: %v", err)
	}

	if err := client.rt.persistRemoteTask(session.ID(), string(turnID), "remote-1", "ctx-1"); err != nil {
		t.Fatalf("persist: %v", err)
	}
	taskID, contextID, ok := client.rt.loadRemoteTask(session.ID())
	if !ok || taskID != "remote-1" || contextID != "ctx-1" {
		t.Fatalf("load = %q, %q, %v", taskID, contextID, ok)
	}

	if err := client.rt.persistRemoteTask(session.ID(), string(turnID), "", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, _, ok := client.rt.loadRemoteTask(session.ID()); ok {
		t.Fatal("remote task was not cleared")
	}
}

// recordingRemote captures the requests it receives and completes immediately.
type recordingRemote struct{ requests []client.Request }

func (r *recordingRemote) Card() client.Card { return client.Card{} }

func (r *recordingRemote) Send(_ context.Context, req client.Request) iter.Seq2[client.Event, error] {
	r.requests = append(r.requests, req)
	return func(yield func(client.Event, error) bool) {
		yield(client.Event{Kind: client.EventCompleted, TaskID: req.TaskID, ContextID: req.ContextID}, nil)
	}
}

func (r *recordingRemote) Cancel(context.Context, string) error { return nil }
func (r *recordingRemote) Close() error                         { return nil }

func TestA2ABackendResumesRecordedRemoteTask(t *testing.T) {
	remote := &recordingRemote{}
	pool := newA2ABackendPool(nil)
	pool.newClient = func(context.Context, string, map[string]string) (client.Client, error) {
		return remote, nil
	}
	pool.loadTask = func(string) (string, string, bool) { return "task-9", "ctx-9", true }
	backend := pool.forAgent(&schema.AgentInfo{Backend: &schema.AgentBackend{
		Type: schema.AgentBackendA2A,
		A2A:  &schema.AgentA2ABackend{Endpoint: "http://remote"},
	}})

	stream, err := backend.Stream(context.Background(), &llmschema.LLMRequest{
		Metadata: map[string]any{runner.RequestMetaSessionID: "ses_1"},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range stream {
	}

	if len(remote.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(remote.requests))
	}
	if remote.requests[0].TaskID != "task-9" || remote.requests[0].ContextID != "ctx-9" {
		t.Fatalf("resume request = %+v", remote.requests[0])
	}
}

func TestRemoteTaskRecordMovesToLatestTurn(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	now := time.Now().UnixMilli()
	turn1, turn2 := schema.NewTurnID(), schema.NewTurnID()
	for _, id := range []schema.TurnID{turn1, turn2} {
		if err := client.TurnStore().Create(schema.TurnInfo{
			ID: id, SessionID: schema.SessionID(session.ID()), State: schema.TurnStateRunning,
			TimeCreated: now, TimeUpdated: now,
		}); err != nil {
			t.Fatalf("create turn: %v", err)
		}
	}

	if err := client.rt.persistRemoteTask(session.ID(), string(turn1), "t1", "c1"); err != nil {
		t.Fatalf("persist t1: %v", err)
	}
	if err := client.rt.persistRemoteTask(session.ID(), string(turn2), "t2", "c2"); err != nil {
		t.Fatalf("persist t2: %v", err)
	}
	if id, _, ok := client.rt.loadRemoteTask(session.ID()); !ok || id != "t2" {
		t.Fatalf("load = %q, %v; want t2", id, ok)
	}
	if info, err := client.TurnStore().Get(turn1); err == nil && info != nil && info.Metadata[turnMetaRemoteTask] != nil {
		t.Fatal("stale remote task record left on the older turn")
	}
	if err := client.rt.persistRemoteTask(session.ID(), string(turn2), "", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, _, ok := client.rt.loadRemoteTask(session.ID()); ok {
		t.Fatal("record not cleared")
	}
}

// failRemote yields a failed remote event immediately.
type failRemote struct{}

func (failRemote) Card() client.Card { return client.Card{} }

func (failRemote) Send(context.Context, client.Request) iter.Seq2[client.Event, error] {
	return func(yield func(client.Event, error) bool) {
		yield(client.Event{Kind: client.EventFailed, TaskID: "t1"}, nil)
	}
}

func (failRemote) Cancel(context.Context, string) error { return nil }
func (failRemote) Close() error                         { return nil }

func TestA2ABackendSurfacesRemoteFailure(t *testing.T) {
	pool := newA2ABackendPool(nil)
	pool.newClient = func(context.Context, string, map[string]string) (client.Client, error) {
		return failRemote{}, nil
	}
	backend := pool.forAgent(&schema.AgentInfo{Backend: &schema.AgentBackend{
		Type: schema.AgentBackendA2A,
		A2A:  &schema.AgentA2ABackend{Endpoint: "http://remote"},
	}})

	stream, err := backend.Stream(context.Background(), &llmschema.LLMRequest{
		Metadata: map[string]any{runner.RequestMetaSessionID: "ses_1"},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	providerError := false
	for event := range stream {
		if event.IsProviderError() {
			providerError = true
		}
	}
	if !providerError {
		t.Fatal("remote failure was not surfaced as a provider error")
	}
}
