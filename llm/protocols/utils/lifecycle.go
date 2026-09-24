package utils

import "github.com/user/go-agent-sdk/llm/schema"

// State tracks the parts a protocol has already started, so each start and end is
// emitted once.
type State struct {
	StepStarted bool
	Text        map[string]struct{}
	Reasoning   map[string]struct{}
}

// Initial returns the state for a new stream.
func Initial() State {
	return State{
		Text:      make(map[string]struct{}),
		Reasoning: make(map[string]struct{}),
	}
}

// StepStart emits the start of a step, once per step.
func StepStart(state *State, events *[]schema.LLMEvent) {
	stepStart(state, events)
}

func stepStart(state *State, events *[]schema.LLMEvent) {
	if state.StepStarted {
		return
	}
	*events = append(*events, schema.NewStepStart(0))
	state.StepStarted = true
}

// TextDelta emits the start of a text part when needed and then the delta.
func TextDelta(state *State, events *[]schema.LLMEvent, id, text string) {
	stepStart(state, events)
	if _, ok := state.Text[id]; ok {
		*events = append(*events, schema.NewTextDelta(id, text))
		return
	}
	*events = append(*events, schema.NewTextStart(id), schema.NewTextDelta(id, text))
	state.Text[id] = struct{}{}
}

// ReasoningStart emits the start of a reasoning part.
func ReasoningStart(state *State, events *[]schema.LLMEvent, id string, providerMetadata ...schema.ProviderMetadata) {
	if _, ok := state.Reasoning[id]; ok {
		return
	}
	stepStart(state, events)
	opts := []schema.EventOption{}
	if len(providerMetadata) > 0 && providerMetadata[0] != nil {
		opts = append(opts, schema.WithEventProviderMetadata(providerMetadata[0]))
	}
	*events = append(*events, schema.NewReasoningStart(id, opts...))
	state.Reasoning[id] = struct{}{}
}

// ReasoningDelta emits the start of a reasoning part when needed and then the
// delta.
func ReasoningDelta(state *State, events *[]schema.LLMEvent, id, text string, providerMetadata ...schema.ProviderMetadata) {
	ReasoningStart(state, events, id, providerMetadata...)
	*events = append(*events, schema.NewReasoningDelta(id, text))
}

// ReasoningEnd emits the end of a reasoning part.
func ReasoningEnd(state *State, events *[]schema.LLMEvent, id string, providerMetadata ...schema.ProviderMetadata) {
	if _, ok := state.Reasoning[id]; !ok {
		return
	}
	stepStart(state, events)
	opts := []schema.EventOption{}
	if len(providerMetadata) > 0 && providerMetadata[0] != nil {
		opts = append(opts, schema.WithEventProviderMetadata(providerMetadata[0]))
	}
	*events = append(*events, schema.NewReasoningEnd(id, opts...))
	delete(state.Reasoning, id)
}

// TextEnd emits the end of a text part.
func TextEnd(state *State, events *[]schema.LLMEvent, id string, providerMetadata ...schema.ProviderMetadata) {
	if _, ok := state.Text[id]; !ok {
		return
	}
	stepStart(state, events)
	opts := []schema.EventOption{}
	if len(providerMetadata) > 0 && providerMetadata[0] != nil {
		opts = append(opts, schema.WithEventProviderMetadata(providerMetadata[0]))
	}
	*events = append(*events, schema.NewTextEnd(id, opts...))
	delete(state.Text, id)
}

func closeOpenBlocks(state *State, events *[]schema.LLMEvent) {
	for id := range state.Reasoning {
		*events = append(*events, schema.NewReasoningEnd(id))
	}
	for id := range state.Text {
		*events = append(*events, schema.NewTextEnd(id))
	}
	state.Text = make(map[string]struct{})
	state.Reasoning = make(map[string]struct{})
}

// Finish emits the end of the step and of the response.
func Finish(state *State, events *[]schema.LLMEvent, input struct {
	Reason           schema.FinishReason
	Usage            *schema.Usage
	ProviderMetadata schema.ProviderMetadata
}) {
	stepStart(state, events)
	closeOpenBlocks(state, events)
	stepFinishOpts := []schema.EventOption{}
	finishOpts := []schema.EventOption{}
	if input.Usage != nil {
		stepFinishOpts = append(stepFinishOpts, schema.WithEventUsage(input.Usage))
		finishOpts = append(finishOpts, schema.WithEventUsage(input.Usage))
	}
	if input.ProviderMetadata != nil {
		stepFinishOpts = append(stepFinishOpts, schema.WithEventProviderMetadata(input.ProviderMetadata))
		finishOpts = append(finishOpts, schema.WithEventProviderMetadata(input.ProviderMetadata))
	}
	*events = append(*events,
		schema.NewStepFinish(0, input.Reason, stepFinishOpts...),
		schema.NewFinish(input.Reason, finishOpts...),
	)
	state.StepStarted = false
}
