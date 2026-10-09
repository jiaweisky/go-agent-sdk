package utils

import "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"

// LifecycleState tracks the parts a protocol has already started, so each start
// and end is emitted once.
type LifecycleState struct {
	StepStarted bool
	Text        map[string]struct{}
	Reasoning   map[string]struct{}
}

// LifecycleInitial returns the state for a new stream.
func LifecycleInitial() LifecycleState {
	return LifecycleState{
		Text:      make(map[string]struct{}),
		Reasoning: make(map[string]struct{}),
	}
}

// StepStart emits the start of a step, once per step.
func StepStart(state *LifecycleState, events *[]schema.LLMEvent) {
	stepStart(state, events)
}

func stepStart(state *LifecycleState, events *[]schema.LLMEvent) {
	if state.StepStarted {
		return
	}
	*events = append(*events, schema.NewStepStart(0))
	state.StepStarted = true
}

// TextDelta emits the start of a text part when needed and then the delta.
func TextDelta(state *LifecycleState, events *[]schema.LLMEvent, id, text string) {
	stepStart(state, events)
	if _, ok := state.Text[id]; ok {
		*events = append(*events, schema.NewTextDelta(id, text))
		return
	}
	*events = append(*events, schema.NewTextStart(id), schema.NewTextDelta(id, text))
	state.Text[id] = struct{}{}
}

// ReasoningStart emits the start of a reasoning part.
func ReasoningStart(
	state *LifecycleState,
	events *[]schema.LLMEvent,
	id string,
	providerMetadata ...schema.ProviderMetadata,
) {
	if _, ok := state.Reasoning[id]; ok {
		return
	}
	stepStart(state, events)
	opts := providerMetadataOptions(optionalMetadata(providerMetadata))
	*events = append(*events, schema.NewReasoningStart(id, opts...))
	state.Reasoning[id] = struct{}{}
}

// ReasoningDelta emits the start of a reasoning part when needed and then the
// delta.
func ReasoningDelta(
	state *LifecycleState,
	events *[]schema.LLMEvent,
	id, text string,
	providerMetadata ...schema.ProviderMetadata,
) {
	ReasoningStart(state, events, id, providerMetadata...)
	*events = append(*events, schema.NewReasoningDelta(id, text))
}

// ReasoningEnd emits the end of a reasoning part.
func ReasoningEnd(
	state *LifecycleState,
	events *[]schema.LLMEvent,
	id string,
	providerMetadata ...schema.ProviderMetadata,
) {
	if _, ok := state.Reasoning[id]; !ok {
		return
	}
	stepStart(state, events)
	opts := providerMetadataOptions(optionalMetadata(providerMetadata))
	*events = append(*events, schema.NewReasoningEnd(id, opts...))
	delete(state.Reasoning, id)
}

// TextEnd emits the end of a text part.
func TextEnd(state *LifecycleState, events *[]schema.LLMEvent, id string, providerMetadata ...schema.ProviderMetadata) {
	if _, ok := state.Text[id]; !ok {
		return
	}
	stepStart(state, events)
	opts := providerMetadataOptions(optionalMetadata(providerMetadata))
	*events = append(*events, schema.NewTextEnd(id, opts...))
	delete(state.Text, id)
}

func optionalMetadata(values []schema.ProviderMetadata) schema.ProviderMetadata {
	if len(values) > 0 {
		return values[0]
	}
	return nil
}

func providerMetadataOptions(pm schema.ProviderMetadata) []schema.EventOption {
	if pm == nil {
		return nil
	}
	return []schema.EventOption{schema.WithEventProviderMetadata(pm)}
}

func closeOpenBlocks(state *LifecycleState, events *[]schema.LLMEvent) {
	for id := range state.Reasoning {
		*events = append(*events, schema.NewReasoningEnd(id))
	}
	for id := range state.Text {
		*events = append(*events, schema.NewTextEnd(id))
	}
	state.Text = make(map[string]struct{})
	state.Reasoning = make(map[string]struct{})
}

// FinishInput is the finish reason, usage and provider metadata of a completed
// step.
type FinishInput struct {
	Reason           schema.FinishReason
	Usage            *schema.Usage
	ProviderMetadata schema.ProviderMetadata
}

// LifecycleFinish emits the end of the step and of the response.
func LifecycleFinish(state *LifecycleState, events *[]schema.LLMEvent, input FinishInput) {
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
