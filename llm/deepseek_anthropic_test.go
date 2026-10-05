package llm

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm/protocols"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

func TestAnthropic_BasicText(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.AnthropicMessagesHTTPRoute.With(route.RoutePatch{
		Auth:     route.Header("x-api-key", os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com/anthropic")},
	})
	rt := route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)}
	req := NewRequest(RequestInput{
		Model:  schema.ModelID("deepseek-flash"),
		Prompt: "Say hello in one word",
	})

	body, err := r.Body.From(req)
	if err != nil {
		t.Fatalf("Body.From: %v", err)
	}
	prepared, err := r.PrepareTransport(body, req)
	if err != nil {
		t.Fatalf("PrepareTransport: %v", err)
	}
	var events []schema.LLMEvent
	for evt, err := range r.StreamPrepared(context.Background(), prepared, req, rt) {
		if err != nil {
			t.Fatalf("StreamPrepared: %v", err)
		}
		events = append(events, evt.(schema.LLMEvent))
	}

	var gotTextDelta bool
	var finish *schema.LLMEvent
	for _, e := range events {
		if e.IsTextDelta() {
			gotTextDelta = true
		}
		if e.IsFinish() {
			finish = &e
		}
		if e.IsProviderError() {
			t.Errorf("unexpected provider error: %s", e.Message)
		}
	}
	if !gotTextDelta {
		t.Error("no TextDelta event")
	}
	if finish == nil {
		t.Fatal("no Finish event")
	}
	if finish.Usage == nil {
		t.Fatal("Finish.Usage is nil")
	}
	if finish.Usage.InputTokens <= 0 {
		t.Errorf("Usage.InputTokens = %d", finish.Usage.InputTokens)
	}
	if finish.Usage.OutputTokens <= 0 {
		t.Errorf("Usage.OutputTokens = %d", finish.Usage.OutputTokens)
	}
}

func TestAnthropic_MultiTurn(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.AnthropicMessagesHTTPRoute.With(route.RoutePatch{
		Auth:     route.Header("x-api-key", os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com/anthropic")},
	})
	rt := route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)}
	req := NewRequest(RequestInput{
		Model: schema.ModelID("deepseek-flash"),
		Messages: []schema.Message{
			schema.MessageSystem("You are helpful."),
			schema.MessageUser("Hello"),
			schema.MessageAssistant("Hi there!"),
			schema.MessageUser("How are you?"),
		},
	})

	body, err := r.Body.From(req)
	if err != nil {
		t.Fatalf("Body.From: %v", err)
	}
	prepared, err := r.PrepareTransport(body, req)
	if err != nil {
		t.Fatalf("PrepareTransport: %v", err)
	}
	var events []schema.LLMEvent
	for evt, err := range r.StreamPrepared(context.Background(), prepared, req, rt) {
		if err != nil {
			t.Fatalf("StreamPrepared: %v", err)
		}
		events = append(events, evt.(schema.LLMEvent))
	}

	var gotTextDelta bool
	var finish *schema.LLMEvent
	for _, e := range events {
		if e.IsTextDelta() {
			gotTextDelta = true
		}
		if e.IsFinish() {
			finish = &e
		}
	}
	if !gotTextDelta {
		t.Error("no TextDelta event")
	}
	if finish == nil {
		t.Fatal("no Finish event")
	}
	if finish.Usage != nil && finish.Usage.InputTokens <= 0 {
		t.Errorf("Usage.InputTokens = %d", finish.Usage.InputTokens)
	}
}

func TestAnthropic_ToolCall(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.AnthropicMessagesHTTPRoute.With(route.RoutePatch{
		Auth:     route.Header("x-api-key", os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com/anthropic")},
	})
	rt := route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)}
	weatherTool := schema.ToolDefinition{
		Name:        "get_weather",
		Description: "Get the current weather for a city",
		InputSchema: schema.JsonSchema{
			"type":       "object",
			"properties": map[string]any{"location": map[string]any{"type": "string"}},
			"required":   []any{"location"},
		},
	}
	req := NewRequest(RequestInput{
		Model:      schema.ModelID("deepseek-flash"),
		Prompt:     "What's the weather in Beijing?",
		Tools:      []schema.ToolDefinition{weatherTool},
		ToolChoice: schema.ToolChoiceAuto,
	})

	body, err := r.Body.From(req)
	if err != nil {
		t.Fatalf("Body.From: %v", err)
	}
	prepared, err := r.PrepareTransport(body, req)
	if err != nil {
		t.Fatalf("PrepareTransport: %v", err)
	}
	var events []schema.LLMEvent
	for evt, err := range r.StreamPrepared(context.Background(), prepared, req, rt) {
		if err != nil {
			t.Fatalf("StreamPrepared: %v", err)
		}
		events = append(events, evt.(schema.LLMEvent))
	}

	var gotToolCall bool
	var finish *schema.LLMEvent
	for _, e := range events {
		if e.IsToolCall() {
			gotToolCall = true
			if e.Name != "get_weather" {
				t.Errorf("ToolCall.Name = %q, want %q", e.Name, "get_weather")
			}
		}
		if e.IsFinish() {
			finish = &e
		}
	}
	if !gotToolCall {
		t.Error("no ToolCall event")
	}
	if finish == nil {
		t.Fatal("no Finish event")
	}
	if finish.Usage != nil && finish.Usage.InputTokens <= 0 {
		t.Errorf("Usage.InputTokens = %d", finish.Usage.InputTokens)
	}
}
