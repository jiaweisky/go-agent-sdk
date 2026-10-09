package schema

// Usage is the token usage of one step, as the provider reported it.
type Usage struct {
	InputTokens           int
	OutputTokens          int
	NonCachedInputTokens  int
	CacheReadInputTokens  int
	CacheWriteInputTokens int
	ReasoningTokens       int
	TotalTokens           int
	// ProviderMetadata carries the provider's original usage payload keyed by
	// provider name (e.g. {"openai": {...raw usage...}}), for fields we don't
	// normalize and for billing-level audit trails.
	ProviderMetadata ProviderMetadata
}

// VisibleOutputTokens is the output tokens the user can see: the output minus the
// reasoning tokens.
func (u *Usage) VisibleOutputTokens() int {
	n := u.OutputTokens - u.ReasoningTokens
	if n < 0 {
		return 0
	}
	return n
}

// LLMEventType is the kind of an event in a streamed response.
type LLMEventType string

const (
	EventStepStart      LLMEventType = "step-start"
	EventTextStart      LLMEventType = "text-start"
	EventTextDelta      LLMEventType = "text-delta"
	EventTextEnd        LLMEventType = "text-end"
	EventReasoningStart LLMEventType = "reasoning-start"
	EventReasoningDelta LLMEventType = "reasoning-delta"
	EventReasoningEnd   LLMEventType = "reasoning-end"
	EventToolInputStart LLMEventType = "tool-input-start"
	EventToolInputDelta LLMEventType = "tool-input-delta"
	EventToolInputEnd   LLMEventType = "tool-input-end"
	EventToolCall       LLMEventType = "tool-call"
	EventToolResult     LLMEventType = "tool-result"
	EventToolError      LLMEventType = "tool-error"
	EventStepFinish     LLMEventType = "step-finish"
	EventFinish         LLMEventType = "finish"
	EventProviderError  LLMEventType = "provider-error"
)

// LLMEvent is one event of a streamed response. The fields each type uses are
// documented on the New* constructors.
type LLMEvent struct {
	Type             LLMEventType
	ID               string
	Index            int
	Text             string
	Name             string
	Input            any
	Result           ToolResultValue
	Output           *ToolOutput
	Message          string
	Error            error
	Reason           FinishReason
	Usage            *Usage
	ProviderExecuted *bool
	Classification   *ProviderFailureClassification
	Retryable        *bool
	ProviderMetadata ProviderMetadata
}

// EventOption sets an optional field of an event.
type EventOption func(*LLMEvent)

// WithEventProviderMetadata attaches provider metadata to the event.
func WithEventProviderMetadata(m ProviderMetadata) EventOption {
	return func(e *LLMEvent) { e.ProviderMetadata = m }
}

// WithEventClassification marks a provider error with its classification.
func WithEventClassification(c ProviderFailureClassification) EventOption {
	return func(e *LLMEvent) { e.Classification = &c }
}

// WithEventProviderExecuted records that the provider, not the SDK, ran the call.
func WithEventProviderExecuted(b bool) EventOption {
	return func(e *LLMEvent) { e.ProviderExecuted = &b }
}

// WithEventOutput attaches a tool output to the event.
func WithEventOutput(o *ToolOutput) EventOption {
	return func(e *LLMEvent) { e.Output = o }
}

// WithEventRetryable marks the event as worth retrying.
func WithEventRetryable(b bool) EventOption {
	return func(e *LLMEvent) { e.Retryable = &b }
}

// WithEventError attaches the cause of a failure.
func WithEventError(err error) EventOption {
	return func(e *LLMEvent) { e.Error = err }
}

// WithEventUsage attaches token usage to the event.
func WithEventUsage(u *Usage) EventOption {
	return func(e *LLMEvent) { e.Usage = u }
}

// NewStepStart reports the start of a step.
func NewStepStart(index int) LLMEvent {
	return LLMEvent{Type: EventStepStart, Index: index}
}

// NewTextStart reports the start of a text part.
func NewTextStart(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventTextStart, ID: id}, opts)
}

// NewTextDelta streams a text chunk.
func NewTextDelta(id, text string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventTextDelta, ID: id, Text: text}, opts)
}

// NewTextEnd reports the end of a text part.
func NewTextEnd(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventTextEnd, ID: id}, opts)
}

// NewReasoningStart reports the start of a reasoning part.
func NewReasoningStart(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventReasoningStart, ID: id}, opts)
}

// NewReasoningDelta streams a reasoning chunk.
func NewReasoningDelta(id, text string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventReasoningDelta, ID: id, Text: text}, opts)
}

// NewReasoningEnd reports the end of a reasoning part.
func NewReasoningEnd(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventReasoningEnd, ID: id}, opts)
}

// NewToolInputStart reports that a tool call's arguments started streaming.
func NewToolInputStart(id, name string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolInputStart, ID: id, Name: name}, opts)
}

// NewToolInputDelta streams a chunk of a tool call's arguments.
func NewToolInputDelta(id, name, text string) LLMEvent {
	return LLMEvent{Type: EventToolInputDelta, ID: id, Name: name, Text: text}
}

// NewToolInputEnd reports that a tool call's arguments finished streaming.
func NewToolInputEnd(id, name string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolInputEnd, ID: id, Name: name}, opts)
}

// NewToolCall reports a complete tool call.
func NewToolCall(id, name string, input any, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolCall, ID: id, Name: name, Input: input}, opts)
}

// NewToolResult reports a tool result.
func NewToolResult(id, name string, result ToolResultValue, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolResult, ID: id, Name: name, Result: result}, opts)
}

// NewToolError reports a tool call that failed.
func NewToolError(id, name, message string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolError, ID: id, Name: name, Message: message}, opts)
}

// NewStepFinish reports the end of a step and why it ended.
func NewStepFinish(index int, reason FinishReason, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventStepFinish, Index: index, Reason: reason}, opts)
}

// NewFinish reports the end of the response and why it ended.
func NewFinish(reason FinishReason, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventFinish, Reason: reason}, opts)
}

// NewProviderErrorEvent reports a provider failure.
func NewProviderErrorEvent(message string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventProviderError, Message: message}, opts)
}

func applyOpts(e LLMEvent, opts []EventOption) LLMEvent {
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// IsStepStart reports whether the event is a step start.
func (e LLMEvent) IsStepStart() bool { return e.Type == EventStepStart }

// IsTextStart reports whether the event is a text start.
func (e LLMEvent) IsTextStart() bool { return e.Type == EventTextStart }

// IsTextDelta reports whether the event is a text delta.
func (e LLMEvent) IsTextDelta() bool { return e.Type == EventTextDelta }

// IsTextEnd reports whether the event is a text end.
func (e LLMEvent) IsTextEnd() bool { return e.Type == EventTextEnd }

// IsReasoningStart reports whether the event is a reasoning start.
func (e LLMEvent) IsReasoningStart() bool { return e.Type == EventReasoningStart }

// IsReasoningDelta reports whether the event is a reasoning delta.
func (e LLMEvent) IsReasoningDelta() bool { return e.Type == EventReasoningDelta }

// IsReasoningEnd reports whether the event is a reasoning end.
func (e LLMEvent) IsReasoningEnd() bool { return e.Type == EventReasoningEnd }

// IsToolInputStart reports whether the event is a tool input start.
func (e LLMEvent) IsToolInputStart() bool { return e.Type == EventToolInputStart }

// IsToolInputDelta reports whether the event is a tool input delta.
func (e LLMEvent) IsToolInputDelta() bool { return e.Type == EventToolInputDelta }

// IsToolInputEnd reports whether the event is a tool input end.
func (e LLMEvent) IsToolInputEnd() bool { return e.Type == EventToolInputEnd }

// IsToolCall reports whether the event is a tool call.
func (e LLMEvent) IsToolCall() bool { return e.Type == EventToolCall }

// IsToolResult reports whether the event is a tool result.
func (e LLMEvent) IsToolResult() bool { return e.Type == EventToolResult }

// IsToolError reports whether the event is a tool error.
func (e LLMEvent) IsToolError() bool { return e.Type == EventToolError }

// IsStepFinish reports whether the event is a step finish.
func (e LLMEvent) IsStepFinish() bool { return e.Type == EventStepFinish }

// IsFinish reports whether the event is a finish.
func (e LLMEvent) IsFinish() bool { return e.Type == EventFinish }

// IsProviderError reports whether the event is a provider error.
func (e LLMEvent) IsProviderError() bool { return e.Type == EventProviderError }

// LLMResponse is a finished response: its message, its events, its usage and why
// it ended.
type LLMResponse struct {
	Message      Message
	Events       []LLMEvent
	Usage        *Usage
	FinishReason FinishReason
}

// Text is the response's text, with the text deltas joined.
func (r *LLMResponse) Text() string {
	var text string
	for _, e := range r.Events {
		if e.IsTextDelta() {
			text += e.Text
		}
	}
	return text
}

// Reasoning is the response's reasoning text, with the deltas joined.
func (r *LLMResponse) Reasoning() string {
	var text string
	for _, e := range r.Events {
		if e.IsReasoningDelta() {
			text += e.Text
		}
	}
	return text
}

// ContentAssembly accumulates a streamed text or reasoning part.
type ContentAssembly struct {
	ContentIndex     int
	Text             string
	ProviderMetadata ProviderMetadata
}

// ToolInputAssembly accumulates a streamed tool call's arguments.
type ToolInputAssembly struct {
	Name             string
	Text             string
	ProviderMetadata ProviderMetadata
}

// ResponseState is the state a response is assembled into, one event at a time.
type ResponseState struct {
	Events         []LLMEvent
	Message        Message
	Usage          *Usage
	FinishReason   *FinishReason
	TextParts      map[string]ContentAssembly
	ReasoningParts map[string]ContentAssembly
	ToolInputs     map[string]ToolInputAssembly
}

// EmptyResponseState returns a state ready to assemble an assistant response.
func EmptyResponseState() ResponseState {
	return ResponseState{
		Events:         nil,
		Message:        Message{Role: RoleAssistant},
		TextParts:      make(map[string]ContentAssembly),
		ReasoningParts: make(map[string]ContentAssembly),
		ToolInputs:     make(map[string]ToolInputAssembly),
	}
}

// LLMResponseComplete returns the response once the state saw a finish reason, or
// nil while the stream is still open.
func LLMResponseComplete(state *ResponseState) *LLMResponse {
	if state.FinishReason == nil {
		return nil
	}
	events := make([]LLMEvent, len(state.Events))
	copy(events, state.Events)
	return &LLMResponse{
		Message:      state.Message,
		Events:       events,
		Usage:        state.Usage,
		FinishReason: *state.FinishReason,
	}
}

// ReduceResponseState folds one event into the response state.
func ReduceResponseState(state *ResponseState, event LLMEvent) {
	state.Events = append(state.Events, event)

	switch event.Type {
	case EventFinish:
		state.FinishReason = &event.Reason
	case EventProviderError:
		if state.FinishReason == nil {
			r := ReasonError
			state.FinishReason = &r
		}
	case EventTextStart:
		ensureText(state, event.ID, event.ProviderMetadata)
	case EventTextDelta:
		reduceTextDelta(state, event)
	case EventTextEnd:
		reduceTextEnd(state, event)
	case EventReasoningStart:
		ensureReasoning(state, event.ID, event.ProviderMetadata)
	case EventReasoningDelta:
		reduceReasoningDelta(state, event)
	case EventReasoningEnd:
		reduceReasoningEnd(state, event)
	case EventToolInputStart:
		reduceToolInputStart(state, event)
	case EventToolInputDelta:
		reduceToolInputDelta(state, event)
	case EventToolInputEnd:
		reduceToolInputEnd(state, event)
	case EventToolCall:
		delete(state.ToolInputs, event.ID)
		state.Message.Content = append(state.Message.Content,
			toolCallContent(event))
	case EventToolResult:
		state.Message.Content = append(state.Message.Content,
			toolResultContent(event))
	}

	if event.Usage != nil {
		state.Usage = event.Usage
	}
}

func reduceToolInputStart(state *ResponseState, event LLMEvent) {
	state.ToolInputs[event.ID] = ToolInputAssembly{
		Name: event.Name, Text: "", ProviderMetadata: event.ProviderMetadata,
	}
}

func reduceToolInputDelta(state *ResponseState, event LLMEvent) {
	cur, ok := state.ToolInputs[event.ID]
	if !ok {
		cur = ToolInputAssembly{Name: event.Name}
	}
	cur.Text += event.Text
	state.ToolInputs[event.ID] = cur
}

func reduceToolInputEnd(state *ResponseState, event LLMEvent) {
	cur, ok := state.ToolInputs[event.ID]
	if !ok {
		cur = ToolInputAssembly{Name: event.Name}
	}
	if event.ProviderMetadata != nil {
		cur.ProviderMetadata = event.ProviderMetadata
	}
	state.ToolInputs[event.ID] = cur
}

func ensureText(state *ResponseState, id string, providerMetadata ProviderMetadata) {
	if _, ok := state.TextParts[id]; ok {
		return
	}
	state.Message.Content = append(state.Message.Content, textContent("", providerMetadata))
	state.TextParts[id] = ContentAssembly{
		ContentIndex:     len(state.Message.Content) - 1,
		ProviderMetadata: providerMetadata,
	}
}

func reduceTextDelta(state *ResponseState, event LLMEvent) {
	ensureText(state, event.ID, event.ProviderMetadata)
	cur, ok := state.TextParts[event.ID]
	if !ok {
		return
	}
	cur.Text += event.Text
	if event.ProviderMetadata != nil {
		cur.ProviderMetadata = event.ProviderMetadata
	}
	state.TextParts[event.ID] = cur
	state.Message.Content[cur.ContentIndex] = textContent(cur.Text, cur.ProviderMetadata)
}

func reduceTextEnd(state *ResponseState, event LLMEvent) {
	cur, ok := state.TextParts[event.ID]
	if !ok {
		return
	}
	if event.ProviderMetadata != nil {
		cur.ProviderMetadata = event.ProviderMetadata
	}
	state.TextParts[event.ID] = cur
	state.Message.Content[cur.ContentIndex] = textContent(cur.Text, cur.ProviderMetadata)
}

func ensureReasoning(state *ResponseState, id string, providerMetadata ProviderMetadata) {
	if _, ok := state.ReasoningParts[id]; ok {
		return
	}
	state.Message.Content = append(state.Message.Content, reasoningContent("", providerMetadata))
	state.ReasoningParts[id] = ContentAssembly{
		ContentIndex:     len(state.Message.Content) - 1,
		ProviderMetadata: providerMetadata,
	}
}

func reduceReasoningDelta(state *ResponseState, event LLMEvent) {
	ensureReasoning(state, event.ID, event.ProviderMetadata)
	cur, ok := state.ReasoningParts[event.ID]
	if !ok {
		return
	}
	cur.Text += event.Text
	if event.ProviderMetadata != nil {
		cur.ProviderMetadata = event.ProviderMetadata
	}
	state.ReasoningParts[event.ID] = cur
	state.Message.Content[cur.ContentIndex] = reasoningContent(cur.Text, cur.ProviderMetadata)
}

func reduceReasoningEnd(state *ResponseState, event LLMEvent) {
	cur, ok := state.ReasoningParts[event.ID]
	if !ok {
		return
	}
	if event.ProviderMetadata != nil {
		cur.ProviderMetadata = event.ProviderMetadata
	}
	state.ReasoningParts[event.ID] = cur
	state.Message.Content[cur.ContentIndex] = reasoningContent(cur.Text, cur.ProviderMetadata)
}

func textContent(text string, providerMetadata ProviderMetadata) ContentPart {
	part := TextPart{Type: "text", Text: text}
	if providerMetadata != nil {
		part.ProviderMetadata = providerMetadata
	}
	return part
}

func reasoningContent(text string, providerMetadata ProviderMetadata) ContentPart {
	part := ReasoningPart{Type: "reasoning", Text: text}
	if providerMetadata != nil {
		part.ProviderMetadata = providerMetadata
	}
	return part
}

func toolCallContent(event LLMEvent) ContentPart {
	return ToolCallPart{
		Type:             "tool-call",
		ID:               event.ID,
		Name:             event.Name,
		Input:            event.Input,
		ProviderExecuted: event.ProviderExecuted,
		ProviderMetadata: event.ProviderMetadata,
	}
}

func toolResultContent(event LLMEvent) ContentPart {
	return ToolResultPart{
		Type:             "tool-result",
		ID:               event.ID,
		Name:             event.Name,
		Result:           event.Result,
		ProviderExecuted: event.ProviderExecuted,
		ProviderMetadata: event.ProviderMetadata,
	}
}

// ToolCalls returns the response's tool calls.
func (r *LLMResponse) ToolCalls() []LLMEvent {
	var calls []LLMEvent
	for _, e := range r.Events {
		if e.IsToolCall() {
			calls = append(calls, e)
		}
	}
	return calls
}

// PreparedRequest is a request a route has compiled: the body it will send and
// what it resolved it from.
type PreparedRequest struct {
	ID       string
	Route    RouteID
	Protocol ProtocolID
	Model    ModelSchema
	Body     any
	Metadata map[string]any
}
