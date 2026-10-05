package agent

import (
	"context"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// recordingLLM records every request and always answers with reply, so a test
// can inspect what the title generation actually sent.
type recordingLLM struct {
	reply string
	mu    sync.Mutex
	reqs  []llmschema.LLMRequest
}

func (r *recordingLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (r *recordingLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (r *recordingLLM) Stream(_ context.Context, req *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, *req)
	r.mu.Unlock()
	events := []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", r.reply),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop),
		llmschema.NewFinish(llmschema.ReasonStop),
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func (r *recordingLLM) requests() []llmschema.LLMRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]llmschema.LLMRequest(nil), r.reqs...)
}

func isTitleRequest(req llmschema.LLMRequest) bool {
	for _, message := range req.Messages {
		for _, part := range message.Content {
			if text, ok := part.(llmschema.TextPart); ok && strings.HasPrefix(text.Text, "Generate a title") {
				return true
			}
		}
	}
	return false
}

// TestSessionAgentsSelectTitleUnderOverride guards that a session-scoped derived
// agent (e.g. /persona) does not hijack the built-in "title" agent lookup.
func TestSessionAgentsSelectTitleUnderOverride(t *testing.T) {
	client := newTestClient(t)
	selector := client.rt.agentSelector
	selector.set("ses_x", schema.AgentInfo{ID: schema.AgentID("build#ses_x"), Mode: schema.AgentModePrimary})

	if got := selector.Select("ses_x", ptr("title")); got.Info == nil || got.ID != "title" {
		t.Fatalf("title select = %+v, want the registry title agent", got)
	}
	if got := selector.Select("ses_x", ptr("build")); got.ID != "build#ses_x" {
		t.Fatalf("build select = %+v, want the derived override", got)
	}
}

// TestSessionTitleGeneratedWhenEnabled guards the opt-in title wiring: with
// Title.Auto set, the first turn's title agent reply becomes the session title
// and a TitleUpdated event is published.
func TestSessionTitleGeneratedWhenEnabled(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model:      "test-model",
		Root:       t.TempDir(),
		Permission: ApproveAll,
		LLM:        fakeLLM{reply: "hello world"},
		Title:      &TitleConfig{Auto: ptr(true)},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	titles := make(chan TitleUpdated, 8)
	client.On(func(event Event) {
		if updated, ok := event.(TitleUpdated); ok {
			select {
			case titles <- updated:
			default:
			}
		}
	})

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	infos, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].Title != "hello world" {
		t.Fatalf("title = %+v, want %q", infos, "hello world")
	}

	select {
	case updated := <-titles:
		if updated.SessionID != session.ID() || updated.Title != "hello world" {
			t.Fatalf("event = %+v", updated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no TitleUpdated event")
	}
}

// TestSessionTitleRequestUsesTitleAgent guards that title generation runs the
// built-in "title" agent's system prompt with no tools.
func TestSessionTitleRequestUsesTitleAgent(t *testing.T) {
	recorder := &recordingLLM{reply: "My Title"}
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model:      "test-model",
		Root:       t.TempDir(),
		Permission: ApproveAll,
		LLM:        recorder,
		Title:      &TitleConfig{Auto: ptr(true)},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()
	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	var titleReq *llmschema.LLMRequest
	for _, request := range recorder.requests() {
		if isTitleRequest(request) {
			captured := request
			titleReq = &captured
		}
	}
	if titleReq == nil {
		t.Fatal("no title request was sent")
	}
	if len(titleReq.Tools) != 0 {
		t.Fatalf("title request sent %d tools, want none", len(titleReq.Tools))
	}
	if len(titleReq.System) == 0 || !strings.Contains(titleReq.System[0].Text, "title generator") {
		t.Fatalf("title request system = %+v, want the title agent prompt", titleReq.System)
	}
}

// TestSessionTitlePreservesUserTitle guards that an explicit title is never
// overwritten by automatic generation.
func TestSessionTitlePreservesUserTitle(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model:      "test-model",
		Root:       t.TempDir(),
		Permission: ApproveAll,
		LLM:        fakeLLM{reply: "generated"},
		Title:      &TitleConfig{Auto: ptr(true)},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Title: "Keep me"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()
	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	infos, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].Title != "Keep me" {
		t.Fatalf("title = %+v, want the user-provided title", infos)
	}
}

// TestSessionSetTitle guards renaming a session after creation: the title is
// persisted and a TitleUpdated event is published, which the chat renders.
func TestSessionSetTitle(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	titles := make(chan TitleUpdated, 8)
	client.On(func(event Event) {
		if updated, ok := event.(TitleUpdated); ok {
			titles <- updated
		}
	})

	if err := session.SetTitle("Renamed"); err != nil {
		t.Fatalf("set title: %v", err)
	}

	infos, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].Title != "Renamed" {
		t.Fatalf("title = %+v, want %q", infos, "Renamed")
	}

	select {
	case updated := <-titles:
		if updated.SessionID != session.ID() || updated.Title != "Renamed" {
			t.Fatalf("event = %+v", updated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no TitleUpdated event")
	}
}

// TestSessionSetTitleMissing guards that renaming a deleted session fails
// instead of silently succeeding.
func TestSessionSetTitleMissing(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()
	if err := client.DeleteSession(context.Background(), session.ID()); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if err := session.SetTitle("x"); err == nil {
		t.Fatal("SetTitle on a deleted session should fail")
	}
}

// TestSessionTitleDisabledByDefault guards that no title is generated without
// the opt-in config, leaving the placeholder title.
func TestSessionTitleDisabledByDefault(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	infos, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || !strings.HasPrefix(infos[0].Title, "New session - ") {
		t.Fatalf("title = %+v, want the default placeholder", infos)
	}
}
