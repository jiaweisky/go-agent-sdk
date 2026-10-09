package a2ago_test

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/a2ago"
	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/client"
	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

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

func startServer(t *testing.T) *httptest.Server {
	t.Helper()
	sdkClient, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: agent.ApproveAll,
		LLM: stubLLM{reply: "remote reply"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = sdkClient.Close() })
	if err := sdkClient.RegisterAgent("build", agent.AgentConfig{
		System: "test", Expose: &schema.AgentExpose{Name: "Remote"},
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

func TestA2AGoClientSendStreaming(t *testing.T) {
	ts := startServer(t)
	remote, err := a2ago.New(context.Background(), ts.URL, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer remote.Close()

	if remote.Card().Name != "Remote" {
		t.Fatalf("card name = %q", remote.Card().Name)
	}
	if !remote.Card().Streaming {
		t.Fatal("card should advertise streaming")
	}

	var (
		text  strings.Builder
		final client.EventKind
	)
	for event, err := range remote.Send(context.Background(), client.Request{Prompt: "hello"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch event.Kind {
		case client.EventArtifact:
			text.WriteString(event.Text)
		case client.EventCompleted, client.EventFailed:
			final = event.Kind
		}
	}
	if text.String() != "remote reply" {
		t.Fatalf("text = %q", text.String())
	}
	if final != client.EventCompleted {
		t.Fatalf("final = %v", final)
	}
}
