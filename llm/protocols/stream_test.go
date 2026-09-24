package protocols

import (
	"context"
	"iter"
	"reflect"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/llm/protocols/utils"
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
	return runStreamRequest(t, r, &schema.LLMRequest{}, frames)
}

// runStreamRequest is runStream with an explicit request (e.g. provider options).
func runStreamRequest(t *testing.T, r route.Route, request *schema.LLMRequest, frames []string) []schema.LLMEvent {
	t.Helper()
	r = r.With(route.RoutePatch{Transport: mockFrames(frames)})
	var events []schema.LLMEvent
	for evt, err := range r.StreamPrepared(context.Background(), nil, request, route.TransportRuntime{}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		events = append(events, evt.(schema.LLMEvent))
	}
	return events
}

func allOfType(events []schema.LLMEvent, typ schema.LLMEventType) []schema.LLMEvent {
	var out []schema.LLMEvent
	for _, e := range events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
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

func reasoningText(events []schema.LLMEvent) string {
	var b strings.Builder
	for _, e := range events {
		if e.Type == schema.EventReasoningDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func TestAnthropicMessagesMockSSEReasoning(t *testing.T) {
	events := runStream(t, AnthropicMessagesHTTPRoute, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"thinking"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"sig_1"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":"\n\nHuman:"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	})

	if got := deltaText(events); got != "Hello!" {
		t.Fatalf("text = %q (events %v)", got, eventTypes(events))
	}
	if got := reasoningText(events); got != "thinking" {
		t.Fatalf("reasoning = %q (events %v)", got, eventTypes(events))
	}
	end := firstOfType(events, schema.EventReasoningEnd)
	if end == nil || end.ProviderMetadata["anthropic"]["signature"] != "sig_1" {
		t.Fatalf("reasoning-end = %+v (events %v)", end, eventTypes(events))
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonStop {
		t.Fatalf("finish = %+v", finish)
	}
	usage := finish.Usage
	if usage == nil || usage.InputTokens != 6 || usage.OutputTokens != 2 ||
		usage.NonCachedInputTokens != 5 || usage.CacheReadInputTokens != 1 || usage.TotalTokens != 8 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestAnthropicMessagesMockSSEToolCall(t *testing.T) {
	events := runStream(t, AnthropicMessagesHTTPRoute, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"lookup"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"weather\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`,
	})

	start := firstOfType(events, schema.EventToolInputStart)
	if start == nil || start.ID != "call_1" || start.Name != "lookup" {
		t.Fatalf("tool-input-start = %+v (events %v)", start, eventTypes(events))
	}
	call := firstOfType(events, schema.EventToolCall)
	if call == nil || call.ID != "call_1" || call.Name != "lookup" {
		t.Fatalf("tool call = %+v (events %v)", call, eventTypes(events))
	}
	input, ok := call.Input.(map[string]any)
	if !ok || input["query"] != "weather" {
		t.Fatalf("tool input = %#v", call.Input)
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonToolCalls {
		t.Fatalf("finish = %+v", finish)
	}
	if finish.Usage == nil || finish.Usage.TotalTokens != 6 {
		t.Fatalf("usage = %+v", finish.Usage)
	}
}

func TestOpenAIResponsesMockSSEReasoning(t *testing.T) {
	events := runStream(t, OpenAIResponsesHTTPRoute, []string{
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","delta":"thinking"}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hello"}`,
		`{"type":"response.reasoning_summary_text.done","item_id":"rs_1"}`,
		`{"type":"response.completed","response":{"id":"resp_1"}}`,
	})

	if got := reasoningText(events); got != "thinking" {
		t.Fatalf("reasoning = %q (events %v)", got, eventTypes(events))
	}
	if got := deltaText(events); got != "Hello" {
		t.Fatalf("text = %q (events %v)", got, eventTypes(events))
	}
	start := firstOfType(events, schema.EventReasoningStart)
	if start == nil || start.ID != "rs_1" {
		t.Fatalf("reasoning-start = %+v (events %v)", start, eventTypes(events))
	}
	end := firstOfType(events, schema.EventReasoningEnd)
	if end == nil || end.ID != "rs_1" {
		t.Fatalf("reasoning-end = %+v (events %v)", end, eventTypes(events))
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonStop {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestOpenAIResponsesMockSSEToolCall(t *testing.T) {
	events := runStream(t, OpenAIResponsesHTTPRoute, []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"lookup","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"{\"query\""}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":":\"weather\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"lookup","arguments":"{\"query\":\"weather\"}"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":1}}}`,
	})

	start := firstOfType(events, schema.EventToolInputStart)
	if start == nil || start.ID != "call_1" || start.Name != "lookup" {
		t.Fatalf("tool-input-start = %+v (events %v)", start, eventTypes(events))
	}
	call := firstOfType(events, schema.EventToolCall)
	if call == nil || call.ID != "call_1" || call.Name != "lookup" {
		t.Fatalf("tool call = %+v (events %v)", call, eventTypes(events))
	}
	input, ok := call.Input.(map[string]any)
	if !ok || input["query"] != "weather" {
		t.Fatalf("tool input = %#v", call.Input)
	}
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.Reason != schema.ReasonToolCalls {
		t.Fatalf("finish = %+v", finish)
	}
}

func reasoningSummaryPartFrames(encrypted string) []string {
	return []string{
		`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1","encrypted_content":null}}`,
		`{"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":0}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","summary_index":0,"delta":"First"}`,
		`{"type":"response.reasoning_summary_part.done","item_id":"rs_1","summary_index":0}`,
		`{"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":1}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","summary_index":1,"delta":"Second"}`,
		`{"type":"response.reasoning_summary_part.done","item_id":"rs_1","summary_index":1}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","encrypted_content":` + encrypted + `}}`,
		`{"type":"response.completed","response":{"id":"resp_1"}}`,
	}
}

func TestOpenAIResponsesMockSSEReasoningSummaryPartsStateless(t *testing.T) {
	off := false
	request := &schema.LLMRequest{ProviderOptions: schema.ProviderOptions{"openai": {"store": off}}}
	events := runStreamRequest(t, OpenAIResponsesHTTPRoute, request, reasoningSummaryPartFrames(`"encrypted-state"`))

	if got := reasoningText(events); got != "FirstSecond" {
		t.Fatalf("reasoning = %q (events %v)", got, eventTypes(events))
	}
	starts := allOfType(events, schema.EventReasoningStart)
	if len(starts) != 2 || starts[0].ID != "rs_1:0" || starts[1].ID != "rs_1:1" {
		t.Fatalf("reasoning starts = %+v", starts)
	}
	ends := allOfType(events, schema.EventReasoningEnd)
	if len(ends) != 2 || ends[0].ID != "rs_1:0" || ends[1].ID != "rs_1:1" {
		t.Fatalf("reasoning ends = %+v", ends)
	}
	if ends[1].ProviderMetadata["openai"]["reasoningEncryptedContent"] != "encrypted-state" {
		t.Fatalf("last reasoning end metadata = %+v", ends[1].ProviderMetadata)
	}
}

func TestOpenAIResponsesMockSSEReasoningSummaryPartsStored(t *testing.T) {
	events := runStream(t, OpenAIResponsesHTTPRoute, reasoningSummaryPartFrames("null"))

	ends := allOfType(events, schema.EventReasoningEnd)
	if len(ends) != 2 || ends[0].ID != "rs_1:0" || ends[1].ID != "rs_1:1" {
		t.Fatalf("reasoning ends = %+v (events %v)", ends, eventTypes(events))
	}
	for _, end := range ends {
		if end.ProviderMetadata["openai"]["itemId"] != "rs_1" {
			t.Fatalf("reasoning end metadata = %+v", end.ProviderMetadata)
		}
	}
}

func TestAnthropicMessagesMockSSEEventSequence(t *testing.T) {
	events := runStream(t, AnthropicMessagesHTTPRoute, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
	})
	want := []schema.LLMEventType{
		schema.EventStepStart,
		schema.EventTextStart,
		schema.EventTextDelta,
		schema.EventTextEnd,
		schema.EventStepFinish,
		schema.EventFinish,
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestAnthropicMessagesMockSSEStopSequence(t *testing.T) {
	events := runStream(t, AnthropicMessagesHTTPRoute, []string{
		`{"type":"message_delta","delta":{"stop_reason":"stop_sequence","stop_sequence":"\n\nHuman:"}}`,
	})
	finish := firstOfType(events, schema.EventFinish)
	if finish == nil || finish.ProviderMetadata["anthropic"]["stopSequence"] != "\n\nHuman:" {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestAnthropicMessagesMockSSEContextOverflow(t *testing.T) {
	events := runStream(t, AnthropicMessagesHTTPRoute, []string{
		`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 210000 tokens > 200000 maximum"}}`,
	})
	evt := firstOfType(events, schema.EventProviderError)
	if evt == nil || evt.Classification == nil || *evt.Classification != schema.ClassificationContextOverflow {
		t.Fatalf("provider error = %+v", evt)
	}
}

func TestOpenAIResponsesMockSSEContextOverflow(t *testing.T) {
	events := runStream(t, OpenAIResponsesHTTPRoute, []string{
		`{"type":"error","code":"context_length_exceeded","message":"too long"}`,
	})
	evt := firstOfType(events, schema.EventProviderError)
	if evt == nil || evt.Classification == nil || *evt.Classification != schema.ClassificationContextOverflow {
		t.Fatalf("provider error = %+v", evt)
	}
}

func TestBedrockConverseErrorClassification(t *testing.T) {
	state := bedrockConverseParserState{tools: utils.Empty(), lifecycle: utils.Initial()}
	cases := []struct {
		name      string
		event     bedrockConverseBedrockEvent
		overflow  bool
		retryable bool
	}{
		{
			name:     "validation context overflow",
			event:    bedrockConverseBedrockEvent{ValidationException: &struct{ Message string }{Message: "Input is too long for requested model"}},
			overflow: true,
		},
		{
			name:  "validation unrelated",
			event: bedrockConverseBedrockEvent{ValidationException: &struct{ Message string }{Message: "invalid tool schema"}},
		},
		{
			name:      "throttling is retryable",
			event:     bedrockConverseBedrockEvent{ThrottlingException: &struct{ Message string }{Message: "slow down"}},
			retryable: true,
		},
		{
			name:      "internal server error is retryable",
			event:     bedrockConverseBedrockEvent{InternalServerException: &struct{ Message string }{Message: "boom"}},
			retryable: true,
		},
	}
	for _, tc := range cases {
		_, events, err := bedrockConverseStep(state, tc.event)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		evt := firstOfType(events, schema.EventProviderError)
		if evt == nil {
			t.Fatalf("%s: no provider error", tc.name)
		}
		gotOverflow := evt.Classification != nil && *evt.Classification == schema.ClassificationContextOverflow
		if gotOverflow != tc.overflow {
			t.Fatalf("%s: classification = %v, want overflow=%v", tc.name, evt.Classification, tc.overflow)
		}
		gotRetryable := evt.Retryable != nil && *evt.Retryable
		if gotRetryable != tc.retryable {
			t.Fatalf("%s: retryable = %v, want %v", tc.name, evt.Retryable, tc.retryable)
		}
	}
}
