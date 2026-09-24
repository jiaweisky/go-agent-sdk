package agent

import (
	"context"
	"iter"
	"testing"
	"time"

	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

func TestUsageEventFromStepEnded(t *testing.T) {
	events := mapEvents(schema.EventPayload{
		Type: schema.SessionEventStepEnded.Type,
		Data: map[string]any{
			"sessionID":          "ses_1",
			"assistantMessageID": "msg_1",
			"finish":             "stop",
			"tokens": map[string]any{
				"input": 100, "output": 15, "reasoning": 5,
				"cache": map[string]any{"read": 30, "write": 10},
				"providerMetadata": map[string]any{
					"openai": map[string]any{
						"prompt_cache_hit_tokens":  30,
						"prompt_cache_miss_tokens": 100,
					},
				},
			},
		},
	})
	var found *Usage
	for i := range events {
		if usage, ok := events[i].(Usage); ok {
			found = &usage
		}
	}
	if found == nil {
		t.Fatalf("no Usage event in %v", events)
	}
	if found.SessionID != "ses_1" {
		t.Fatalf("session = %q", found.SessionID)
	}
	// The raw provider usage survives verbatim (JSON numbers decode as float64).
	openai := found.ProviderMetadata["openai"]
	if openai["prompt_cache_hit_tokens"] != float64(30) ||
		openai["prompt_cache_miss_tokens"] != float64(100) {
		t.Fatalf("providerMetadata = %+v", found.ProviderMetadata)
	}
}

// usageEventLLM reports a provider usage object on its step finish.
type usageEventLLM struct{ usage *llmschema.Usage }

func (u usageEventLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (u usageEventLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (u usageEventLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return eventSeq([]llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", "hi"),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop, llmschema.WithEventUsage(u.usage)),
		llmschema.NewFinish(llmschema.ReasonStop),
	}), nil
}

func TestUsageEventDeliveredViaOn(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM: usageEventLLM{usage: &llmschema.Usage{
			NonCachedInputTokens: 100, OutputTokens: 20, ReasoningTokens: 5,
			CacheReadInputTokens: 30, CacheWriteInputTokens: 10,
			ProviderMetadata: llmschema.ProviderMetadata{
				"openai": {"prompt_cache_hit_tokens": 30, "prompt_cache_miss_tokens": 100},
			},
		}},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	usage := make(chan Usage, 4)
	unsubscribe := client.On(func(event Event) {
		if value, ok := event.(Usage); ok && value.SessionID == session.ID() {
			usage <- value
		}
	})
	defer unsubscribe()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case got := <-usage:
			openai := got.ProviderMetadata["openai"]
			if openai["prompt_cache_hit_tokens"] != float64(30) ||
				openai["prompt_cache_miss_tokens"] != float64(100) {
				t.Fatalf("providerMetadata = %+v", got.ProviderMetadata)
			}
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("no Usage event delivered via On")
}
