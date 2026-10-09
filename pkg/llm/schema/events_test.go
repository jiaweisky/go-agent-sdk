package schema

import "testing"

func TestReduceResponseStateKeepsUsageWhenFinishHasNone(t *testing.T) {
	state := EmptyResponseState()
	ReduceResponseState(&state, LLMEvent{Type: EventStepFinish, Usage: &Usage{InputTokens: 5}})
	ReduceResponseState(&state, LLMEvent{Type: EventFinish, Reason: ReasonStop})

	if state.Usage == nil || state.Usage.InputTokens != 5 {
		t.Fatalf("usage = %+v, want the earlier step usage", state.Usage)
	}
	if state.FinishReason == nil || *state.FinishReason != ReasonStop {
		t.Fatalf("finish reason = %v", state.FinishReason)
	}
}

func TestReduceResponseStateKeepsProviderMetadata(t *testing.T) {
	state := EmptyResponseState()
	pm := ProviderMetadata{"anthropic": map[string]any{"signature": "sig_1"}}
	ReduceResponseState(&state, LLMEvent{Type: EventReasoningStart, ID: "reasoning-0"})
	ReduceResponseState(&state, LLMEvent{Type: EventReasoningDelta, ID: "reasoning-0", Text: "thinking", ProviderMetadata: pm})
	ReduceResponseState(&state, LLMEvent{Type: EventReasoningEnd, ID: "reasoning-0", ProviderMetadata: pm})

	if len(state.Message.Content) != 1 {
		t.Fatalf("content = %+v", state.Message.Content)
	}
	part, ok := state.Message.Content[0].(ReasoningPart)
	if !ok || part.ProviderMetadata["anthropic"]["signature"] != "sig_1" {
		t.Fatalf("reasoning part = %+v", state.Message.Content[0])
	}
}

func TestReduceResponseStateKeepsProviderExecuted(t *testing.T) {
	state := EmptyResponseState()
	executed := true
	ReduceResponseState(&state, LLMEvent{
		Type:             EventToolCall,
		ID:               "call_1",
		Name:             "web_search",
		ProviderExecuted: &executed,
	})

	if len(state.Message.Content) != 1 {
		t.Fatalf("content = %+v", state.Message.Content)
	}
	part, ok := state.Message.Content[0].(ToolCallPart)
	if !ok || part.ProviderExecuted == nil || !*part.ProviderExecuted {
		t.Fatalf("tool call part = %+v", state.Message.Content[0])
	}
}

func TestReduceResponseStateProviderErrorSetsReason(t *testing.T) {
	state := EmptyResponseState()
	ReduceResponseState(&state, LLMEvent{Type: EventProviderError, Message: "boom"})
	if state.FinishReason == nil || *state.FinishReason != ReasonError {
		t.Fatalf("finish reason = %v, want error", state.FinishReason)
	}

	// An existing finish reason is not overwritten by a later error.
	state = EmptyResponseState()
	ReduceResponseState(&state, LLMEvent{Type: EventFinish, Reason: ReasonStop})
	ReduceResponseState(&state, LLMEvent{Type: EventProviderError, Message: "boom"})
	if state.FinishReason == nil || *state.FinishReason != ReasonStop {
		t.Fatalf("finish reason = %v, want stop", state.FinishReason)
	}
}
