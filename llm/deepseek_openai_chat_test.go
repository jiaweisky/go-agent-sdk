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

func chatRuntime() route.TransportRuntime {
	return route.TransportRuntime{HTTP: route.NewExecutor(http.DefaultClient)}
}

func collectEvents(t *testing.T, r route.Route, req *schema.LLMRequest, rt route.TransportRuntime) []schema.LLMEvent {
	t.Helper()

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
	return events
}

func TestChat_BasicText(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Auth:     route.Bearer(os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com")},
	})
	req := NewRequest(RequestInput{
		Model:  schema.ModelID("deepseek-flash"),
		Prompt: "Say hello in one word",
	})
	events := collectEvents(t, r, req, chatRuntime())

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
	if finish.Reason != schema.ReasonStop {
		t.Errorf("Finish.Reason = %q, want %q", finish.Reason, schema.ReasonStop)
	}
	if finish.Usage == nil {
		t.Fatal("Finish.Usage is nil")
	}
	if finish.Usage.InputTokens <= 0 {
		t.Errorf("Usage.InputTokens = %d, want > 0", finish.Usage.InputTokens)
	}
	if finish.Usage.OutputTokens <= 0 {
		t.Errorf("Usage.OutputTokens = %d, want > 0", finish.Usage.OutputTokens)
	}
}

func TestChat_MultiTurn(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Auth:     route.Bearer(os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com")},
	})
	req := NewRequest(RequestInput{
		Model: schema.ModelID("deepseek-flash"),
		Messages: []schema.Message{
			schema.MessageSystem("You are a poet. Answer in rhyme."),
			schema.MessageUser("What color is the sky?"),
			schema.MessageAssistant("The sky is blue, as clear as can be."),
			schema.MessageUser("What about the sun?"),
		},
	})
	events := collectEvents(t, r, req, chatRuntime())

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
		t.Errorf("Usage.InputTokens = %d, want > 0", finish.Usage.InputTokens)
	}
}

func TestChat_ToolCall(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Auth:     route.Bearer(os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com")},
	})
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
	events := collectEvents(t, r, req, chatRuntime())

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
	if finish.Reason != schema.ReasonToolCalls {
		t.Errorf("Finish.Reason = %q, want %q", finish.Reason, schema.ReasonToolCalls)
	}
	if finish.Usage != nil && finish.Usage.InputTokens <= 0 {
		t.Errorf("Usage.InputTokens = %d", finish.Usage.InputTokens)
	}
}

func TestChat_GenerationParams(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("set DEEPSEEK_API_KEY to run this test")
	}
	r := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Auth:     route.Bearer(os.Getenv("DEEPSEEK_API_KEY")),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com")},
	})
	req := NewRequest(RequestInput{
		Model:  schema.ModelID("deepseek-flash"),
		Prompt: "Count from 1 to 10",
		Generation: &schema.GenerationOptions{
			Temperature: ptrF64(0.0),
			MaxTokens:   ptr(30),
			Stop:        []string{"3"},
		},
	})
	events := collectEvents(t, r, req, chatRuntime())

	var finish *schema.LLMEvent
	for _, e := range events {
		if e.IsFinish() {
			finish = &e
		}
	}
	if finish == nil {
		t.Fatal("no Finish event")
	}
	if finish.Usage != nil && finish.Usage.InputTokens <= 0 {
		t.Errorf("Usage.InputTokens = %d", finish.Usage.InputTokens)
	}
}

func TestChat_InvalidAuth(t *testing.T) {
	r := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Auth:     route.Bearer("sk-invalid"),
		Endpoint: &route.EndpointPatch{BaseURL: ptr("https://api.deepseek.com")},
	})
	req := NewRequest(RequestInput{
		Model:  schema.ModelID("deepseek-flash"),
		Prompt: "hello",
	})
	rt := chatRuntime()
	body, err := r.Body.From(req)
	if err != nil {
		t.Fatalf("Body.From: %v", err)
	}
	prepared, err := r.PrepareTransport(body, req)
	if err != nil {
		t.Fatalf("PrepareTransport: %v", err)
	}
	for evt, err := range r.StreamPrepared(context.Background(), prepared, req, rt) {
		if err != nil {
			if llmErr, ok := err.(*schema.LLMError); ok {
				if llmErr.Reason.Kind != schema.ReasonAuthentication {
					t.Errorf("expected authentication error, got reason kind %d", llmErr.Reason.Kind)
				}
			} else {
				t.Logf("expected auth error, got: %v", err)
			}
			return
		}
		_ = evt
	}
	t.Error("expected an error but got none")
}

func ptr[T any](v T) *T         { return &v }
func ptrF64(v float64) *float64 { return &v }
