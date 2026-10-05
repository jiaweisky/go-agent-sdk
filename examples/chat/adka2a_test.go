package main

import (
	"context"
	"iter"
	"strings"
	"testing"

	agent "github.com/user/go-agent-sdk"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
)

// adkStubLLM is an unused local model: the session runs the remote ADK agent,
// so the local loop never streams.
type adkStubLLM struct{}

func (adkStubLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (adkStubLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (adkStubLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return func(func(llmschema.LLMEvent, error) bool) {}, nil
}

// TestAdkA2AInterop is the cross-implementation test: the go-agent-sdk client
// connects to a Google ADK agent served over A2A (the examples/adka2a
// submodule) and asserts the round trip carried the prompt.
func TestAdkA2AInterop(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the adka2a submodule")
	}
	url, stop, err := startAdkA2A()
	if err != nil {
		t.Fatalf("start adk a2a: %v", err)
	}
	defer stop()

	client, err := agent.NewClient(&agent.ClientOptions{
		Protocol: "openai-responses", Credentials: agent.APIKey("test"),
		Model: "test-model", Root: t.TempDir(), Permission: agent.ApproveAll,
		LLM: adkStubLLM{},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := registerRemoteAgent(client, url); err != nil {
		t.Fatalf("register echo agent: %v", err)
	}

	session, err := client.CreateSession(context.Background(), &agent.SessionConfig{Agent: echoAgentName})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var text strings.Builder
	for event, err := range session.Send(context.Background(), agent.MessageOptions{Prompt: "ping from sdk"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case agent.TextDelta:
			text.WriteString(e.Delta)
		case agent.TextEnded:
			text.WriteString(e.Text)
		}
	}
	if !strings.Contains(text.String(), "ping from sdk") {
		t.Fatalf("adk reply = %q; want it to echo the prompt", text.String())
	}
}
