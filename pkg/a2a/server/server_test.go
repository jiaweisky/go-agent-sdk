package server_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// stubLLM returns a fixed assistant reply.
type stubLLM struct{ reply string }

func (s stubLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (s stubLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (s stubLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	events := []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", s.reply),
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

func newServer(t *testing.T) (*httptest.Server, *agent.Client) {
	t.Helper()
	return newServerWithSecurity(t, nil)
}

func newServerWithSecurity(t *testing.T, security *server.Security) (*httptest.Server, *agent.Client) {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol:    "openai-responses",
		Credentials: agent.APIKey("test"),
		Model:       "test-model",
		Root:        t.TempDir(),
		Permission:  agent.ApproveAll,
		LLM:         stubLLM{reply: "hello from a2a"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	expose := &schema.AgentExpose{Name: "Echo", Description: "echoes"}
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "You are a test agent.", Expose: expose,
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Create the httptest server first so the card can carry its absolute URL.
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	srv, err := server.New(server.Options{
		Factory:  agent.NewA2AFactory(client),
		Agents:   client.Agents().All(),
		BaseURL:  ts.URL,
		Security: security,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	handler = srv.Handler()
	return ts, client
}

func fetchCard(t *testing.T, baseURL string) *a2a.AgentCard {
	t.Helper()
	response, err := http.Get(baseURL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("get card: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("card status: %d", response.StatusCode)
	}
	var card a2a.AgentCard
	if err := json.NewDecoder(response.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	return &card
}

// TestCardPaths checks each exposed agent's card path, including a custom
// Expose.Path override.
func TestCardPaths(t *testing.T) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: agent.ApproveAll,
		LLM: stubLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for id, expose := range map[string]*schema.AgentExpose{
		"build":  {Name: "Build"},
		"review": {Name: "Review", Path: "/custom/review.json"},
	} {
		if err := client.RegisterAgent(id, agent.AgentConfig{Expose: expose}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	srv, err := server.New(server.Options{Factory: agent.NewA2AFactory(client), Agents: client.Agents().All()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	got := srv.CardPaths()
	want := map[string]bool{
		"/agents/build/.well-known/agent-card.json": true,
		"/custom/review.json":                       true,
	}
	if len(got) != len(want) {
		t.Fatalf("CardPaths = %v, want %d entries", got, len(want))
	}
	for _, path := range got {
		if !want[path] {
			t.Errorf("unexpected card path %q", path)
		}
	}
}

// TestServerServesAggregateCards guards the discovery endpoint that returns all
// exposed agents' cards as a JSON array, the shape CodeBuddy's
// CODEBUDDY_REMOTE_AGENTS_URL expects.
func TestServerServesAggregateCards(t *testing.T) {
	ts, _ := newServer(t)

	response, err := http.Get(ts.URL + server.DiscoveryPath)
	if err != nil {
		t.Fatalf("get cards: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cards status: %d", response.StatusCode)
	}
	var cards []a2a.AgentCard
	if err := json.NewDecoder(response.Body).Decode(&cards); err != nil {
		t.Fatalf("decode cards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	if got := cards[0].SupportedInterfaces; len(got) != 1 || got[0].URL != ts.URL+"/agents/build/" {
		t.Fatalf("interfaces = %v, want 1 at %s", got, ts.URL+"/agents/build/")
	}
}

// TestServerCardsAccessor guards the exported Cards accessor a multiplexing host
// uses to aggregate cards across server instances onto one origin.
func TestServerCardsAccessor(t *testing.T) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol:    "openai-responses",
		Credentials: agent.APIKey("test"),
		Model:       "test-model",
		Root:        t.TempDir(),
		Permission:  agent.ApproveAll,
		LLM:         stubLLM{reply: "hi"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", agent.AgentConfig{
		Expose: &schema.AgentExpose{Name: "Echo"},
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	srv, err := server.New(server.Options{
		Factory: agent.NewA2AFactory(client),
		Agents:  client.Agents().All(),
		BaseURL: "https://agents.example.com",
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	cards := srv.Cards(nil)
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	if got := cards[0].SupportedInterfaces; len(got) != 1 || got[0].URL != "https://agents.example.com/agents/build/" {
		t.Fatalf("interfaces = %v, want 1 at %s", got, "https://agents.example.com/agents/build/")
	}
}

// TestServerRequiresBearerToken guards that the RPC endpoint is gated while the
// discovery card stays public and declares the scheme.
func TestServerRequiresBearerToken(t *testing.T) {
	ts, _ := newServerWithSecurity(t, &server.Security{Bearer: "t0ken"})

	card := fetchCard(t, ts.URL)
	if _, ok := card.SecuritySchemes["bearer"]; !ok {
		t.Fatalf("card securitySchemes = %v, want bearer", card.SecuritySchemes)
	}

	endpoint := ts.URL + "/agents/build/"

	response, err := http.Post(endpoint, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer t0ken")
	authorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("authorized post: %v", err)
	}
	authorized.Body.Close()
	if authorized.StatusCode == http.StatusUnauthorized {
		t.Fatalf("authorized status = %d, want not 401", authorized.StatusCode)
	}
}

func TestServerSendStreamingMessage(t *testing.T) {
	ts, _ := newServer(t)
	card := fetchCard(t, ts.URL)

	a2aclient, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("new a2a client: %v", err)
	}
	defer a2aclient.Destroy()

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	message.ContextID = a2a.NewContextID()
	request := &a2a.SendMessageRequest{Message: message}

	var (
		artifact strings.Builder
		state    a2a.TaskState
	)
	for event, err := range a2aclient.SendStreamingMessage(context.Background(), request) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			state = e.Status.State
		case *a2a.TaskArtifactUpdateEvent:
			for _, part := range e.Artifact.Parts {
				artifact.WriteString(part.Text())
			}
		case *a2a.TaskStatusUpdateEvent:
			state = e.Status.State
		}
	}

	if artifact.String() != "hello from a2a" {
		t.Fatalf("artifact = %q", artifact.String())
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("final state = %s", state)
	}
}

func TestServerGetTask(t *testing.T) {
	ts, _ := newServer(t)
	card := fetchCard(t, ts.URL)

	a2aclient, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("new a2a client: %v", err)
	}
	defer a2aclient.Destroy()

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	message.ContextID = a2a.NewContextID()
	result, err := a2aclient.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: message})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("result = %T, want *a2a.Task", result)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", task.Status.State)
	}

	stored, err := a2aclient.GetTask(context.Background(), &a2a.GetTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if stored.ID != task.ID {
		t.Fatalf("stored id = %s, want %s", stored.ID, task.ID)
	}
}

// questionLLM asks the user a question on the first call, then answers.
type questionLLM struct {
	calls *int
	reply string
}

func (f questionLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f questionLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f questionLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	*f.calls++
	var events []llmschema.LLMEvent
	if *f.calls == 1 {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewToolCall("call-q", "question", map[string]any{
				"questions": []any{map[string]any{
					"question": "Pick a color",
					"header":   "color",
					"options": []any{
						map[string]any{"label": "red", "description": "warm"},
						map[string]any{"label": "blue", "description": "cool"},
					},
				}},
			}),
			llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
			llmschema.NewFinish(llmschema.ReasonToolCalls),
		}
	} else {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewTextStart("t1"),
			llmschema.NewTextDelta("t1", f.reply),
			llmschema.NewTextEnd("t1"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
			llmschema.NewFinish(llmschema.ReasonStop),
		}
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func newQuestionServer(t *testing.T) *httptest.Server {
	t.Helper()
	calls := 0
	sdkClient, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(),
		LLM: questionLLM{calls: &calls, reply: "answered"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = sdkClient.Close() })
	if err := sdkClient.RegisterAgent("build", agent.AgentConfig{
		System: "test", Expose: &schema.AgentExpose{Name: "Ask"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	srv, err := server.New(server.Options{
		Factory: agent.NewA2AFactory(sdkClient),
		Agents:  sdkClient.Agents().All(),
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	handler = srv.Handler()
	return ts
}

func TestServerInputRequiredRoundTrip(t *testing.T) {
	ts := newQuestionServer(t)
	card := fetchCard(t, ts.URL)
	a2aclient, err := a2aclient.NewFromCard(context.Background(), card)
	if err != nil {
		t.Fatalf("new a2a client: %v", err)
	}
	defer a2aclient.Destroy()

	contextID := a2a.NewContextID()
	first := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("start"))
	first.ContextID = contextID

	var (
		taskID string
		state  a2a.TaskState
	)
	for event, err := range a2aclient.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: first}) {
		if err != nil {
			t.Fatalf("first stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			taskID = string(e.ID)
			state = e.Status.State
		case *a2a.TaskStatusUpdateEvent:
			state = e.Status.State
		case *a2a.TaskArtifactUpdateEvent:
			taskID = string(e.TaskID)
		}
	}
	if state != a2a.TaskStateInputRequired {
		t.Fatalf("first state = %s, want input-required", state)
	}
	if taskID == "" {
		t.Fatal("no task id")
	}

	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("blue"))
	reply.TaskID = a2a.TaskID(taskID)
	reply.ContextID = contextID
	var artifact strings.Builder
	for event, err := range a2aclient.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: reply}) {
		if err != nil {
			t.Fatalf("resume stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			state = e.Status.State
		case *a2a.TaskStatusUpdateEvent:
			state = e.Status.State
		case *a2a.TaskArtifactUpdateEvent:
			for _, part := range e.Artifact.Parts {
				artifact.WriteString(part.Text())
			}
		}
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("final state = %s", state)
	}
	if !strings.Contains(artifact.String(), "answered") {
		t.Fatalf("artifact = %q", artifact.String())
	}
}

func TestServerDerivesCardEndpointFromRequest(t *testing.T) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: agent.ApproveAll,
		LLM: stubLLM{reply: "hi"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "test", Expose: &schema.AgentExpose{Name: "Derived"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	// No BaseURL: the card endpoint must come from the request's origin.
	srv, err := server.New(server.Options{
		Factory: agent.NewA2AFactory(client),
		Agents:  client.Agents().All(),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	card := fetchCard(t, ts.URL)
	if len(card.SupportedInterfaces) != 1 {
		t.Fatalf("interfaces = %d, want 1", len(card.SupportedInterfaces))
	}
	if want := ts.URL + "/agents/build/"; card.SupportedInterfaces[0].URL != want {
		t.Fatalf("endpoint = %q, want %q", card.SupportedInterfaces[0].URL, want)
	}
}

func TestServerRestartRebuildsParkedTurn(t *testing.T) {
	root := t.TempDir()
	calls := 0
	newHost := func() *agent.Client {
		c, err := agent.NewClient(&agent.ClientOptions{
			Protocol: "openai-responses", Credentials: agent.APIKey("test"),
			Model: "test-model", Root: root,
			LLM: questionLLM{calls: &calls, reply: "answered after restart"},
		})
		if err != nil {
			t.Fatalf("new client: %v", err)
		}
		if err := c.RegisterAgent("build", agent.AgentConfig{
			System: "test", Expose: &schema.AgentExpose{Name: "Restart"},
		}); err != nil {
			t.Fatalf("register: %v", err)
		}
		return c
	}

	// First process: the turn parks at input-required, then the process "dies".
	host1 := newHost()
	srv1, err := server.New(server.Options{Factory: agent.NewA2AFactory(host1), Agents: host1.Agents().All(), Turns: host1.TurnStore()})
	if err != nil {
		t.Fatalf("server1: %v", err)
	}
	ts1 := httptest.NewServer(srv1.Handler())
	client1, err := a2aclient.NewFromCard(context.Background(), fetchCard(t, ts1.URL))
	if err != nil {
		t.Fatalf("client1: %v", err)
	}
	contextID := a2a.NewContextID()
	first := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("start"))
	first.ContextID = contextID
	var taskID string
	var state a2a.TaskState
	for event, err := range client1.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: first}) {
		if err != nil {
			t.Fatalf("first stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			taskID, state = string(e.ID), e.Status.State
		case *a2a.TaskStatusUpdateEvent:
			state = e.Status.State
		}
	}
	if state != a2a.TaskStateInputRequired || taskID == "" {
		t.Fatalf("first leg state=%s task=%q", state, taskID)
	}
	client1.Destroy()
	ts1.Close()
	if err := host1.Close(); err != nil {
		t.Fatalf("close host1: %v", err)
	}

	// Second process: same database, no in-memory parked turn. The reply must
	// rebuild the turn from the durable task and complete.
	host2 := newHost()
	t.Cleanup(func() { _ = host2.Close() })
	srv2, err := server.New(server.Options{Factory: agent.NewA2AFactory(host2), Agents: host2.Agents().All(), Turns: host2.TurnStore()})
	if err != nil {
		t.Fatalf("server2: %v", err)
	}
	ts2 := httptest.NewServer(srv2.Handler())
	t.Cleanup(ts2.Close)
	client2, err := a2aclient.NewFromCard(context.Background(), fetchCard(t, ts2.URL))
	if err != nil {
		t.Fatalf("client2: %v", err)
	}
	defer client2.Destroy()

	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("blue"))
	reply.TaskID = a2a.TaskID(taskID)
	reply.ContextID = contextID
	var artifact strings.Builder
	for event, err := range client2.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: reply}) {
		if err != nil {
			t.Fatalf("resume stream: %v", err)
		}
		switch e := event.(type) {
		case *a2a.Task:
			state = e.Status.State
		case *a2a.TaskStatusUpdateEvent:
			state = e.Status.State
		case *a2a.TaskArtifactUpdateEvent:
			for _, part := range e.Artifact.Parts {
				artifact.WriteString(part.Text())
			}
		}
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("final state = %s", state)
	}
	if !strings.Contains(artifact.String(), "answered after restart") {
		t.Fatalf("artifact = %q", artifact.String())
	}
}

func TestServerAcceptsNativeOptions(t *testing.T) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: agent.ApproveAll,
		LLM: stubLLM{reply: "native ok"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "test", Expose: &schema.AgentExpose{Name: "Native"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	srv, err := server.New(server.Options{
		Factory: agent.NewA2AFactory(client),
		Agents:  client.Agents().All(),
		// A real a2a-go option passes through; an unknown value is ignored.
		NativeOptions: []any{a2asrv.WithAgentInactivityTimeout(time.Minute), struct{}{}},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	a2aclient, err := a2aclient.NewFromCard(context.Background(), fetchCard(t, ts.URL))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer a2aclient.Destroy()

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	message.ContextID = a2a.NewContextID()
	state := a2a.TaskState("")
	for event, err := range a2aclient.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if status, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
			state = status.Status.State
		}
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", state)
	}
}

func TestServerWritesInboundAttachment(t *testing.T) {
	workspace := t.TempDir()
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), WorkingDirectory: workspace,
		Permission: agent.ApproveAll, LLM: stubLLM{reply: "ok"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "test", Expose: &schema.AgentExpose{Name: "Attach"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv, err := server.New(server.Options{Factory: agent.NewA2AFactory(client), Agents: client.Agents().All()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	a2aclient, err := a2aclient.NewFromCard(context.Background(), fetchCard(t, ts.URL))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer a2aclient.Destroy()

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("see attached"), a2a.NewRawPart([]byte("payload")))
	message.ContextID = a2a.NewContextID()
	state := a2a.TaskState("")
	for event, err := range a2aclient.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if status, ok := event.(*a2a.TaskStatusUpdateEvent); ok {
			state = status.Status.State
		}
	}
	if state != a2a.TaskStateCompleted {
		t.Fatalf("state = %s", state)
	}

	var written string
	_ = filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.Contains(path, "attachments") {
			written = path
		}
		return nil
	})
	if written == "" {
		t.Fatal("inbound attachment was not written to the workspace")
	}
	if data, err := os.ReadFile(written); err != nil || string(data) != "payload" {
		t.Fatalf("attachment content = %q, %v", data, err)
	}
}

func TestServerRecordsSingleTurn(t *testing.T) {
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: agent.ApproveAll,
		LLM: stubLLM{reply: "ok"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", agent.AgentConfig{
		System: "test", Expose: &schema.AgentExpose{Name: "Turn"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv, err := server.New(server.Options{
		Factory: agent.NewA2AFactory(client),
		Agents:  client.Agents().All(),
		Turns:   client.TurnStore(),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	a2aclient, err := a2aclient.NewFromCard(context.Background(), fetchCard(t, ts.URL))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer a2aclient.Destroy()

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	message.ContextID = a2a.NewContextID()
	for event, err := range a2aclient.SendStreamingMessage(context.Background(), &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		_ = event
	}

	sessions, err := client.ListSessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %d, %v; want 1", len(sessions), err)
	}
	turns, err := client.TurnStore().ListForSession(schema.SessionID(sessions[0].ID))
	if err != nil {
		t.Fatalf("list turns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1 (an inbound task must not also create a runner turn)", len(turns))
	}
	if turns[0].AnchorSeq == nil || turns[0].EndSeq == nil {
		t.Fatalf("inbound turn is missing anchor/end sequence: %+v", turns[0])
	}
}
