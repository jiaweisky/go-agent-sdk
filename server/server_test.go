package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/core"
	coresession "github.com/jiaweisky/go-agent-sdk/core/session"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/mcp"
	"github.com/jiaweisky/go-agent-sdk/schema"
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

	// Prompt from a goroutine (plain http, so no t.Fatalf off the test
	// goroutine), after a short delay so the SSE subscription is in place;
	// assert the streamed text on the test goroutine.
	go func() {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Post(ts.URL+"/api/session/"+created.Data.ID+"/prompt", "application/json", strings.NewReader(`{"prompt":"hi"}`))
		if err == nil {
			resp.Body.Close()
		}
	}()
	readSSE(t, ts, "/api/session/"+created.Data.ID+"/event", "hello world")
}

func createSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session", `{}`, nil)
	defer resp.Body.Close()
	var created struct {
		Data sessionInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("create decode: %v", err)
	}
	return created.Data.ID
}

func waitIdle(t *testing.T, ts *httptest.Server, id string) {
	t.Helper()
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/wait?timeout=10s", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wait status = %d", resp.StatusCode)
	}
}

// userCount GETs the active context and counts user messages.
func userCount(t *testing.T, ts *httptest.Server, id string) int {
	t.Helper()
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/context", "", nil)
	defer resp.Body.Close()
	var body struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	count := 0
	for _, message := range body.Data {
		if message["type"] == "user" {
			count++
		}
	}
	return count
}

func poll(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fn()
}

func TestPromptIdempotency(t *testing.T) {
	ts := newTestServer(t, Options{})
	id := createSession(t, ts)

	// Two prompts with the same client id must produce one turn.
	for i := 0; i < 2; i++ {
		resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"hi","id":"msg_same"}`, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("prompt status = %d", resp.StatusCode)
		}
	}
	if !poll(t, 5*time.Second, func() bool { return userCount(t, ts, id) >= 1 }) {
		t.Fatal("no user message observed")
	}
	waitIdle(t, ts, id)
	if count := userCount(t, ts, id); count != 1 {
		t.Fatalf("user messages = %d, want 1 (idempotent admission)", count)
	}
}

func TestSessionActiveAndHistory(t *testing.T) {
	ts := newTestServer(t, Options{})
	id := createSession(t, ts)

	// /active is a literal route, not a session lookup.
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/session/active", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("active status = %d", resp.StatusCode)
	}

	resp = doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"hi"}`, nil)
	resp.Body.Close()

	ok := poll(t, 5*time.Second, func() bool {
		resp := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/history", "", nil)
		defer resp.Body.Close()
		var history struct {
			Data []wireEvent `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&history)
		return len(history.Data) > 0
	})
	if !ok {
		t.Fatal("history returned no events")
	}
}

// serveClient wraps a client + server into an httptest server with cleanup.
func serveClient(t *testing.T, client *agent.Client, opts Options) *httptest.Server {
	t.Helper()
	t.Cleanup(func() { _ = client.Close() })
	srv, err := New(client, opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// blockingLLM keeps a turn in flight until release is closed.
type blockingLLM struct{ release chan struct{} }

func (b blockingLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (b blockingLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (b blockingLLM) Stream(ctx context.Context, _ *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return func(yield func(llmschema.LLMEvent, error) bool) {
		select {
		case <-b.release:
		case <-ctx.Done():
			return
		}
		for _, event := range []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewTextStart("t1"),
			llmschema.NewTextDelta("t1", "done"),
			llmschema.NewTextEnd("t1"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
			llmschema.NewFinish(llmschema.ReasonStop),
		} {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func TestSessionActiveList(t *testing.T) {
	release := make(chan struct{})
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), LLM: blockingLLM{release: release},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	srv, err := New(client, Options{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	id := createSession(t, ts)
	go func() {
		resp, err := http.Post(ts.URL+"/api/session/"+id+"/prompt", "application/json", strings.NewReader(`{"prompt":"hi"}`))
		if err == nil {
			resp.Body.Close()
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	seen := false
	for time.Now().Before(deadline) && !seen {
		resp := doJSON(t, http.MethodGet, ts.URL+"/api/session/active", "", nil)
		var body struct {
			Data map[string]map[string]string `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if entry, ok := body.Data[id]; ok && entry["type"] == "running" {
			seen = true
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	if !seen {
		t.Fatal("active session was not listed while running")
	}
}

func activeContains(t *testing.T, ts *httptest.Server, id string) bool {
	t.Helper()
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/session/active", "", nil)
	defer resp.Body.Close()
	var body struct {
		Data map[string]map[string]string `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	_, ok := body.Data[id]
	return ok
}

func TestCORSPreflight(t *testing.T) {
	ts := newTestServer(t, Options{})
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/session", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("allow-origin = %q", got)
	}
}

func TestCORSConfiguredOrigin(t *testing.T) {
	ts := newTestServer(t, Options{CORS: []string{"https://app.example.com"}})

	preflight := func(origin string) string {
		req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/session", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		defer resp.Body.Close()
		return resp.Header.Get("Access-Control-Allow-Origin")
	}

	if got := preflight("https://app.example.com"); got != "https://app.example.com" {
		t.Fatalf("configured origin not allowed: %q", got)
	}
	if got := preflight("https://evil.example.com"); got != "" {
		t.Fatalf("unlisted non-loopback origin was allowed: %q", got)
	}
}

func TestPublicRouteOverridesAuth(t *testing.T) {
	ts := newTestServer(t, Options{Auth: &Auth{Scheme: schemeBearer, Token: "t"}, Public: []string{"/api/agent"}})

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/agent", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public route status = %d, want 200", resp.StatusCode)
	}

	resp = doJSON(t, http.MethodGet, ts.URL+"/api/session", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected route status = %d, want 401", resp.StatusCode)
	}
}

func TestSessionMessagePagination(t *testing.T) {
	ts := newTestServer(t, Options{})
	id := createSession(t, ts)
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"hi"}`, nil)
	resp.Body.Close()
	if !poll(t, 5*time.Second, func() bool {
		r := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/history", "", nil)
		defer r.Body.Close()
		var body struct {
			Data []wireEvent `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, event := range body.Data {
			if event.Type == "session.next.text.ended" {
				return true
			}
		}
		return false
	}) {
		t.Fatal("turn did not finish")
	}

	first := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/message?limit=1", "", nil)
	var page1 struct {
		Data   []map[string]any `json:"data"`
		Cursor struct {
			Next string `json:"next"`
		} `json:"cursor"`
	}
	if err := json.NewDecoder(first.Body).Decode(&page1); err != nil {
		t.Fatalf("page1 decode: %v", err)
	}
	first.Body.Close()
	if len(page1.Data) != 1 || page1.Cursor.Next == "" {
		t.Fatalf("page1 = %d items, next=%q", len(page1.Data), page1.Cursor.Next)
	}

	second := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/message?limit=1&cursor="+page1.Cursor.Next, "", nil)
	var page2 struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(second.Body).Decode(&page2); err != nil {
		t.Fatalf("page2 decode: %v", err)
	}
	second.Body.Close()
	if len(page2.Data) != 1 || page2.Data[0]["id"] == page1.Data[0]["id"] {
		t.Fatalf("page2 = %v (want a different single message)", page2.Data)
	}
}

func TestBasicAuth(t *testing.T) {
	ts := newTestServer(t, Options{Auth: &Auth{Scheme: schemeBasic, Username: "u", Password: "p"}})

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/agent", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent", nil)
	req.SetBasicAuth("u", "p")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("auth status = %d", resp2.StatusCode)
	}
}

func TestRequestValidation(t *testing.T) {
	ts := newTestServer(t, Options{})
	id := createSession(t, ts)
	for _, tc := range []struct{ path, body string }{
		{"/api/session/" + id + "/model", `{}`},
		{"/api/session/" + id + "/revert/stage", `{}`},
		{"/api/session/" + id + "/prompt", `{}`},
	} {
		resp := doJSON(t, http.MethodPost, ts.URL+tc.path, tc.body, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", tc.path, resp.StatusCode)
		}
	}
}

func TestWaitTimeout(t *testing.T) {
	release := make(chan struct{})
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), LLM: blockingLLM{release: release},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ts := serveClient(t, client, Options{})
	id := createSession(t, ts)
	go func() {
		resp, err := http.Post(ts.URL+"/api/session/"+id+"/prompt", "application/json", strings.NewReader(`{"prompt":"hi"}`))
		if err == nil {
			resp.Body.Close()
		}
	}()
	if !poll(t, 5*time.Second, func() bool { return activeContains(t, ts, id) }) {
		t.Fatal("turn did not start")
	}

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/wait?timeout=200ms", "", nil)
	defer resp.Body.Close()
	var body struct {
		Data struct {
			Idle bool `json:"idle"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("wait decode: %v", err)
	}
	close(release)
	if body.Data.Idle {
		t.Fatal("wait reported idle while the turn was still running")
	}
}

func TestDeleteWhileRunning(t *testing.T) {
	release := make(chan struct{})
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), LLM: blockingLLM{release: release},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ts := serveClient(t, client, Options{})
	id := createSession(t, ts)
	go func() {
		resp, err := http.Post(ts.URL+"/api/session/"+id+"/prompt", "application/json", strings.NewReader(`{"prompt":"hi"}`))
		if err == nil {
			resp.Body.Close()
		}
	}()
	if !poll(t, 5*time.Second, func() bool { return activeContains(t, ts, id) }) {
		t.Fatal("turn did not start")
	}

	resp := doJSON(t, http.MethodDelete, ts.URL+"/api/session/"+id, "", nil)
	resp.Body.Close()
	close(release)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	after := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id, "", nil)
	after.Body.Close()
	if after.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", after.StatusCode)
	}
}

func TestSessionPagination(t *testing.T) {
	ts := newTestServer(t, Options{})
	for i := 0; i < 3; i++ {
		createSession(t, ts)
	}

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/session?limit=2", "", nil)
	var first struct {
		Data   []sessionInfoDTO `json:"data"`
		Cursor struct {
			Next string `json:"next"`
		} `json:"cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatalf("page1 decode: %v", err)
	}
	resp.Body.Close()
	if len(first.Data) != 2 || first.Cursor.Next == "" {
		t.Fatalf("page1 = %d items, next=%q", len(first.Data), first.Cursor.Next)
	}

	resp = doJSON(t, http.MethodGet, ts.URL+"/api/session?limit=2&cursor="+first.Cursor.Next, "", nil)
	var second struct {
		Data []sessionInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&second); err != nil {
		t.Fatalf("page2 decode: %v", err)
	}
	resp.Body.Close()
	if len(second.Data) != 1 {
		t.Fatalf("page2 = %d items, want 1", len(second.Data))
	}
	if second.Data[0].ID == first.Data[0].ID || second.Data[0].ID == first.Data[1].ID {
		t.Fatal("page2 overlapped page1")
	}
}

// TestMCPToolMaterialization builds the repo's notes MCP server, connects it as
// a local stdio server and checks that its tools surface through /api/tool.
func TestMCPToolMaterialization(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the notes MCP server")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "notesmcp")
	build := exec.Command("go", "build", "-o", binary, "./examples/notesmcp")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build notesmcp: %v\n%s", err, out)
	}

	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("k"), Model: "test-model",
		Root: t.TempDir(),
		MCP: []agent.MCPConfig{{Name: "notes", Server: mcp.ServerConfig{
			Type:    "local",
			Command: []string{binary, "--data", filepath.Join(t.TempDir(), "notes.json")},
		}}},
		LLM: fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ts := serveClient(t, client, Options{})

	resp := doJSON(t, http.MethodGet, ts.URL+"/api/tool?agent=build", "", nil)
	defer resp.Body.Close()
	var body struct {
		Data []toolInfoDTO `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("tool decode: %v", err)
	}
	for _, tool := range body.Data {
		if strings.Contains(tool.Name, "note_list") {
			return
		}
	}
	t.Fatalf("MCP tool not materialized; tools = %+v", body.Data)
}

func TestErrorMapping(t *testing.T) {
	srv, err := New(newTestClient(t), Options{OnError: func(error) {}})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	cases := []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: ses_x", agent.ErrSessionNotFound), http.StatusNotFound},
		{fmt.Errorf("%w: nope", agent.ErrUnknownAgent), http.StatusBadRequest},
		{agent.ErrSessionBusy, http.StatusConflict},
		{fmt.Errorf("%w for this project", core.ErrSnapshotUnavailable), http.StatusServiceUnavailable},
		{&coresession.RevertMessageNotFoundError{MessageID: "msg_x"}, http.StatusNotFound},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		srv.writeSDKError(rec, tc.err)
		if rec.Code != tc.want {
			t.Errorf("writeSDKError(%v) = %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}

func TestSessionReplay(t *testing.T) {
	ts := newTestServer(t, Options{})
	id := createSession(t, ts)

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"hi"}`, nil)
	resp.Body.Close()

	// Wait until the durable history carries the finished turn, then a
	// reconnecting client must get it from replay (?after=0).
	if !poll(t, 5*time.Second, func() bool {
		r := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/history", "", nil)
		defer r.Body.Close()
		var body struct {
			Data []wireEvent `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return len(body.Data) > 0
	}) {
		t.Fatal("history stayed empty")
	}
	readSSE(t, ts, "/api/session/"+id+"/event?after=0", "text.ended")
}

func TestInteractionTimeoutFallback(t *testing.T) {
	calls := 0
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("k"),
		Model: "test-model", Root: t.TempDir(),
		Tools: []agent.Tool{agent.DefineTool("echo", "echo", func(struct{}, agent.ToolContext) (string, error) {
			return "ok", nil
		})},
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.RegisterAgent("build", agent.AgentConfig{
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := serveClient(t, client, Options{
		InteractionTimeout:  200 * time.Millisecond,
		InteractionFallback: "reject",
	})
	id := createSession(t, ts)

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"go"}`, nil)
	resp.Body.Close()

	// The ask must first appear, then the watcher resolves it with no reply.
	if !poll(t, 5*time.Second, func() bool { return permissionCount(t, ts, id) > 0 }) {
		t.Fatal("no permission request appeared")
	}
	if !poll(t, 8*time.Second, func() bool { return permissionCount(t, ts, id) == 0 }) {
		t.Fatal("interaction timeout did not resolve the pending ask")
	}
}

func permissionCount(t *testing.T, ts *httptest.Server, id string) int {
	t.Helper()
	r := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/permission", "", nil)
	defer r.Body.Close()
	var body struct {
		Data []permissionRequestDTO `json:"data"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return len(body.Data)
}

func TestInteractionFallbackAllow(t *testing.T) {
	calls := 0
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("k"),
		Model: "test-model", Root: t.TempDir(),
		Tools: []agent.Tool{agent.DefineTool("echo", "echo", func(struct{}, agent.ToolContext) (string, error) {
			return "ok", nil
		})},
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.RegisterAgent("build", agent.AgentConfig{
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := serveClient(t, client, Options{
		InteractionTimeout:  200 * time.Millisecond,
		InteractionFallback: "allow",
	})
	id := createSession(t, ts)

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"go"}`, nil)
	resp.Body.Close()

	// With no reply, the fallback approves once and the tool still runs.
	if !poll(t, 8*time.Second, func() bool {
		r := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/history", "", nil)
		defer r.Body.Close()
		var body struct {
			Data []wireEvent `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, event := range body.Data {
			if event.Type == "session.next.tool.success" {
				return true
			}
		}
		return false
	}) {
		t.Fatal("allow fallback did not approve the ask")
	}
}

// toolCallLLM calls a tool on its first stream and returns text on the second,
// so a test can drive a tool (and its permission ask) then complete the turn.
type toolCallLLM struct {
	calls    *int
	toolName string
	reply    string
}

func (f toolCallLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f toolCallLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f toolCallLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	*f.calls++
	var events []llmschema.LLMEvent
	if *f.calls == 1 {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewToolCall("call-1", f.toolName, map[string]any{}),
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

func TestPermissionRoundTrip(t *testing.T) {
	calls := 0
	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("k"),
		Model: "test-model", Root: t.TempDir(),
		Tools: []agent.Tool{agent.DefineTool("echo", "echo", func(struct{}, agent.ToolContext) (string, error) {
			return "ok", nil
		})},
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.RegisterAgent("build", agent.AgentConfig{
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := serveClient(t, client, Options{})
	id := createSession(t, ts)

	resp := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/prompt", `{"prompt":"go"}`, nil)
	resp.Body.Close()

	// The tool call parks on an ask; the client observes it and replies.
	var requestID string
	if !poll(t, 5*time.Second, func() bool {
		r := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/permission", "", nil)
		defer r.Body.Close()
		var body struct {
			Data []permissionRequestDTO `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Data) > 0 {
			requestID = body.Data[0].ID
			return true
		}
		return false
	}) {
		t.Fatal("no permission request appeared")
	}

	reply := doJSON(t, http.MethodPost, ts.URL+"/api/session/"+id+"/permission/"+requestID+"/reply", `{"reply":"once"}`, nil)
	reply.Body.Close()
	if reply.StatusCode != http.StatusNoContent {
		t.Fatalf("reply status = %d", reply.StatusCode)
	}

	// The turn resumes after the reply and completes the tool call.
	if !poll(t, 5*time.Second, func() bool {
		r := doJSON(t, http.MethodGet, ts.URL+"/api/session/"+id+"/history", "", nil)
		defer r.Body.Close()
		var body struct {
			Data []wireEvent `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, event := range body.Data {
			if event.Type == "session.next.tool.success" {
				return true
			}
		}
		return false
	}) {
		t.Fatal("turn did not resume after the permission reply")
	}
}
