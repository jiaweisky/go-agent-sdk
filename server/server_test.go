package server

import (
	"bufio"
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// fakeLLM streams a single text part and finishes, so a turn completes without a
// network call.
type fakeLLM struct{ reply string }

func (f fakeLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f fakeLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f fakeLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	events := []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", f.reply),
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

func newTestClient(t *testing.T) *agent.Client {
	t.Helper()
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(),
		LLM: fakeLLM{reply: "hello world"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newTestServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	srv, err := New(newTestClient(t), opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func doJSON(t *testing.T, method, url string, body string, headers map[string]string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t, Options{})
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/health", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAuth(t *testing.T) {
	ts := newTestServer(t, Options{
		Auth:   &Auth{Scheme: schemeBearer, Token: "t0ken"},
		Public: []string{"/api/health"},
	})
	// public route needs no auth
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/health", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	// protected route without a token
	resp = doJSON(t, http.MethodGet, ts.URL+"/api/agent", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", resp.StatusCode)
	}
	// protected route with a token
	resp = doJSON(t, http.MethodGet, ts.URL+"/api/agent", "", map[string]string{"Authorization": "Bearer t0ken"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth status = %d", resp.StatusCode)
	}
}

func TestAgentAllowList(t *testing.T) {
	ts := newTestServer(t, Options{Agents: []string{"plan"}})

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/agent", "", nil)
	var list struct {
		Data []agentInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(list.Data) != 1 || list.Data[0].ID != "plan" {
		t.Fatalf("agents = %+v, want only plan", list.Data)
	}

	resp = doJSON(t, http.MethodPost, ts.URL+"/api/session", `{"agent":"build"}`, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed agent status = %d, want 403", resp.StatusCode)
	}
}

func TestSessionLifecycle(t *testing.T) {
	ts := newTestServer(t, Options{})

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session", `{}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var created struct {
		Data sessionInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	resp.Body.Close()
	if created.Data.ID == "" {
		t.Fatal("created session has no id")
	}

	resp = doJSON(t, http.MethodGet, ts.URL+"/api/session/"+created.Data.ID, "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, http.MethodDelete, ts.URL+"/api/session/"+created.Data.ID, "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	resp = doJSON(t, http.MethodGet, ts.URL+"/api/session/"+created.Data.ID, "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", resp.StatusCode)
	}
}

func TestUnknownAgent(t *testing.T) {
	ts := newTestServer(t, Options{})
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session", `{"agent":"nope"}`, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestNotFoundAndMethodNotAllowedEnvelope(t *testing.T) {
	ts := newTestServer(t, Options{})
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/nope", "", nil)
	var e apiError
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatalf("decode 404: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || e.Error != "not_found" {
		t.Fatalf("404 envelope = %d %+v", resp.StatusCode, e)
	}

	resp = doJSON(t, http.MethodDelete, ts.URL+"/api/health", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("405 status = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatalf("decode 405: %v", err)
	}
	if e.Error != "method_not_allowed" {
		t.Fatalf("405 envelope = %+v", e)
	}
}

// readSSE reads SSE frames until want appears or the deadline passes.
func readSSE(t *testing.T, ts *httptest.Server, path, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), want) {
			return
		}
	}
	t.Fatalf("sse stream ended without %q", want)
}

func TestGlobalEventConnected(t *testing.T) {
	ts := newTestServer(t, Options{})
	readSSE(t, ts, "/api/event", "server.connected")
}

func TestPromptTurnEvents(t *testing.T) {
	ts := newTestServer(t, Options{})

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session", `{}`, nil)
	var created struct {
		Data sessionInfoDTO `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// open the per-session stream, then prompt; the turn's text should stream.
	done := make(chan struct{})
	go func() {
		defer close(done)
		readSSE(t, ts, "/api/session/"+created.Data.ID+"/event", "hello world")
	}()
	// give the stream a moment to subscribe before prompting
	time.Sleep(100 * time.Millisecond)

	resp = doJSON(t, http.MethodPost, ts.URL+"/api/session/"+created.Data.ID+"/prompt", `{"prompt":"hi"}`, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("prompt status = %d", resp.StatusCode)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("did not observe the turn's text on the session stream")
	}
}
