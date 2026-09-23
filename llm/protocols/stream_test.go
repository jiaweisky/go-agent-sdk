package protocols

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// mockFrames is a route.Transport that replays the given SSE frame payloads (the
// decoded `data:` strings) without any network access.
type mockFrames []string

func (mockFrames) ID() string                                       { return "mock" }
func (mockFrames) Prepare(route.TransportPrepareInput) (any, error) { return nil, nil }
func (m mockFrames) Frames(context.Context, any, *schema.LLMRequest, route.TransportRuntime) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for _, frame := range m {
			if !yield(frame, nil) {
				return
			}
		}
	}
}

// runStream drives a route's stream over mock SSE frames and returns the events.
func runStream(t *testing.T, r route.Route, frames []string) []schema.LLMEvent {
	t.Helper()
	r = r.With(route.RoutePatch{Transport: mockFrames(frames)})
	var events []schema.LLMEvent
	for evt, err := range r.StreamPrepared(context.Background(), nil, &schema.LLMRequest{}, route.TransportRuntime{}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		events = append(events, evt.(schema.LLMEvent))
	}
	return events
}

func eventTypes(events []schema.LLMEvent) []schema.LLMEventType {
	types := make([]schema.LLMEventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

func deltaText(events []schema.LLMEvent) string {
	var b strings.Builder
	for _, e := range events {
		if e.Type == schema.EventTextDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func firstOfType(events []schema.LLMEvent, typ schema.LLMEventType) *schema.LLMEvent {
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

func TestOpenAIChatMockSSEText(t *testing.T) {
	events := runStream(t, OpenAIChatHTTPRoute, []string{
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
	})

	if got := deltaText(events); got != "Hello world" {
		t.Fatalf("text = %q, want %q (events %v)", got, "Hello world", eventTypes(events))
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonStop {
		t.Fatalf("finish = %+v (events %v)", finish, eventTypes(events))
	}
	if finish.Usage == nil || finish.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", finish.Usage)
	}
}

func TestOpenAIChatMockSSEToolCall(t *testing.T) {
	events := runStream(t, OpenAIChatHTTPRoute, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	})

	call := firstOfType(events, schema.EventToolCall)
	if call == nil || call.Name != "get_weather" {
		t.Fatalf("tool call = %+v (events %v)", call, eventTypes(events))
	}
	input, ok := call.Input.(map[string]any)
	if !ok || input["city"] != "Paris" {
		t.Fatalf("tool input = %#v", call.Input)
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonToolCalls {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestAnthropicMessagesMockSSEText(t *testing.T) {
	events := runStream(t, AnthropicMessagesHTTPRoute, []string{
		`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":3,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	})

	if got := deltaText(events); got != "Hello" {
		t.Fatalf("text = %q (events %v)", got, eventTypes(events))
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonStop {
		t.Fatalf("finish = %+v (events %v)", finish, eventTypes(events))
	}
}

func TestGeminiMockSSEText(t *testing.T) {
	events := runStream(t, GeminiHTTPRoute, []string{
		`{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}`,
		`{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`,
	})

	if got := deltaText(events); got != "Hello" {
		t.Fatalf("text = %q (events %v)", got, eventTypes(events))
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonStop {
		t.Fatalf("finish = %+v (events %v)", finish, eventTypes(events))
	}
}

func TestOpenAIResponsesMockSSEText(t *testing.T) {
	events := runStream(t, OpenAIResponsesHTTPRoute, []string{
		`{"type":"response.output_text.delta","delta":"Hello"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
	})

	if got := deltaText(events); got != "Hello" {
		t.Fatalf("text = %q (events %v)", got, eventTypes(events))
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonStop {
		t.Fatalf("finish = %+v (events %v)", finish, eventTypes(events))
	}
}

func TestGeminiMockSSEToolCall(t *testing.T) {
	events := runStream(t, GeminiHTTPRoute, []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]}}]}`,
		`{"candidates":[{"finishReason":"STOP"}]}`,
	})

	call := firstOfType(events, schema.EventToolCall)
	if call == nil || call.Name != "get_weather" {
		t.Fatalf("tool call = %+v (events %v)", call, eventTypes(events))
	}
	input, ok := call.Input.(map[string]any)
	if !ok || input["city"] != "Paris" {
		t.Fatalf("tool input = %#v", call.Input)
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonToolCalls {
		t.Fatalf("finish = %+v", finish)
	}
}
