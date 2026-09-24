package schema

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
	// normalize and for billing-level audit trails. Mirrors provider-error.ts.
	ProviderMetadata ProviderMetadata
}

func (u *Usage) VisibleOutputTokens() int {
	n := u.OutputTokens - u.ReasoningTokens
	if n < 0 {
		return 0
	}
	return n
}

func UsageFrom(input any) *Usage {
	switch v := input.(type) {
	case *Usage:
		return v
	case Usage:
		return &v
	default:
		return nil
	}
}

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

type EventOption func(*LLMEvent)

func WithEventProviderMetadata(m ProviderMetadata) EventOption {
	return func(e *LLMEvent) { e.ProviderMetadata = m }
}

func WithEventClassification(c ProviderFailureClassification) EventOption {
	return func(e *LLMEvent) { e.Classification = &c }
}

func WithEventProviderExecuted(b bool) EventOption {
	return func(e *LLMEvent) { e.ProviderExecuted = &b }
}

func WithEventOutput(o *ToolOutput) EventOption {
	return func(e *LLMEvent) { e.Output = o }
}

func WithEventRetryable(b bool) EventOption {
	return func(e *LLMEvent) { e.Retryable = &b }
}

func WithEventError(err error) EventOption {
	return func(e *LLMEvent) { e.Error = err }
}

func WithEventUsage(u *Usage) EventOption {
	return func(e *LLMEvent) { e.Usage = u }
}

func NewStepStart(index int) LLMEvent {
	return LLMEvent{Type: EventStepStart, Index: index}
}

func NewTextStart(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventTextStart, ID: id}, opts)
}

func NewTextDelta(id, text string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventTextDelta, ID: id, Text: text}, opts)
}

func NewTextEnd(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventTextEnd, ID: id}, opts)
}

func NewReasoningStart(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventReasoningStart, ID: id}, opts)
}

func NewReasoningDelta(id, text string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventReasoningDelta, ID: id, Text: text}, opts)
}

func NewReasoningEnd(id string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventReasoningEnd, ID: id}, opts)
}

func NewToolInputStart(id, name string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolInputStart, ID: id, Name: name}, opts)
}

func NewToolInputDelta(id, name, text string) LLMEvent {
	return LLMEvent{Type: EventToolInputDelta, ID: id, Name: name, Text: text}
}

func NewToolInputEnd(id, name string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolInputEnd, ID: id, Name: name}, opts)
}

func NewToolCall(id, name string, input any, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolCall, ID: id, Name: name, Input: input}, opts)
}

func NewToolResult(id, name string, result ToolResultValue, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolResult, ID: id, Name: name, Result: result}, opts)
}

func NewToolError(id, name, message string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventToolError, ID: id, Name: name, Message: message}, opts)
}

func NewStepFinish(index int, reason FinishReason, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventStepFinish, Index: index, Reason: reason}, opts)
}

func NewFinish(reason FinishReason, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventFinish, Reason: reason}, opts)
}

func NewProviderErrorEvent(message string, opts ...EventOption) LLMEvent {
	return applyOpts(LLMEvent{Type: EventProviderError, Message: message}, opts)
}

func applyOpts(e LLMEvent, opts []EventOption) LLMEvent {
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func (e LLMEvent) IsStepStart() bool      { return e.Type == EventStepStart }
func (e LLMEvent) IsTextStart() bool      { return e.Type == EventTextStart }
func (e LLMEvent) IsTextDelta() bool      { return e.Type == EventTextDelta }
func (e LLMEvent) IsTextEnd() bool        { return e.Type == EventTextEnd }
func (e LLMEvent) IsReasoningStart() bool { return e.Type == EventReasoningStart }
func (e LLMEvent) IsReasoningDelta() bool { return e.Type == EventReasoningDelta }
func (e LLMEvent) IsReasoningEnd() bool   { return e.Type == EventReasoningEnd }
func (e LLMEvent) IsToolInputStart() bool { return e.Type == EventToolInputStart }
func (e LLMEvent) IsToolInputDelta() bool { return e.Type == EventToolInputDelta }
func (e LLMEvent) IsToolInputEnd() bool   { return e.Type == EventToolInputEnd }
func (e LLMEvent) IsToolCall() bool       { return e.Type == EventToolCall }
func (e LLMEvent) IsToolResult() bool     { return e.Type == EventToolResult }
func (e LLMEvent) IsToolError() bool      { return e.Type == EventToolError }
func (e LLMEvent) IsStepFinish() bool     { return e.Type == EventStepFinish }
func (e LLMEvent) IsFinish() bool         { return e.Type == EventFinish }
func (e LLMEvent) IsProviderError() bool  { return e.Type == EventProviderError }

type LLMResponse struct {
	Message      Message
	Events       []LLMEvent
	Usage        *Usage
	FinishReason FinishReason
}

func (r *LLMResponse) Text() string {
	var text string
	for _, e := range r.Events {
		if e.IsTextDelta() {
			text += e.Text
		}
	}
	return text
}

func (r *LLMResponse) Reasoning() string {
	var text string
	for _, e := range r.Events {
		if e.IsReasoningDelta() {
			text += e.Text
		}
	}
	return text
}

type ContentAssembly struct {
	ContentIndex     int
	Text             string
	ProviderMetadata ProviderMetadata
}

type ToolInputAssembly struct {
	Name             string
	Text             string
	ProviderMetadata ProviderMetadata
}

type ResponseState struct {
	Events         []LLMEvent
	Message        Message
	Usage          *Usage
	FinishReason   *FinishReason
	TextParts      map[string]ContentAssembly
	ReasoningParts map[string]ContentAssembly
	ToolInputs     map[string]ToolInputAssembly
}

func EmptyResponseState() ResponseState {
	return ResponseState{
		Events:         nil,
		Message:        Message{Role: RoleAssistant},
		TextParts:      make(map[string]ContentAssembly),
		ReasoningParts: make(map[string]ContentAssembly),
		ToolInputs:     make(map[string]ToolInputAssembly),
	}
}

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

func LLMResponseFromEvents(events []LLMEvent) *LLMResponse {
	state := EmptyResponseState()
	for _, e := range events {
		ReduceResponseState(&state, e)
	}
	return LLMResponseComplete(&state)
}

func LLMResponseText(r *LLMResponse) string {
	var text string
	for _, e := range r.Events {
		if e.IsTextDelta() {
			text += e.Text
		}
	}
	return text
}

func LLMResponseUsage(r *LLMResponse) *Usage {
	if r.Usage != nil {
		return r.Usage
	}
	for _, e := range r.Events {
		if e.Usage != nil {
			return e.Usage
		}
	}
	return nil
}

func LLMResponseReasoning(r *LLMResponse) string {
	var text string
	for _, e := range r.Events {
		if e.IsReasoningDelta() {
			text += e.Text
		}
	}
	return text
}

func LLMResponseToolCalls(r *LLMResponse) []LLMEvent {
	return r.ToolCalls()
}

func ReduceResponseState(state *ResponseState, event LLMEvent) {
	state.Events = append(state.Events, event)

	switch event.Type {
	case EventFinish:
		if event.Usage != nil {
			state.Usage = event.Usage
		}
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
		state.ToolInputs[event.ID] = ToolInputAssembly{
			Name: event.Name, Text: "", ProviderMetadata: event.ProviderMetadata,
		}
	case EventToolInputDelta:
		cur, ok := state.ToolInputs[event.ID]
		if !ok {
			cur = ToolInputAssembly{Name: event.Name}
		}
		cur.Text += event.Text
		state.ToolInputs[event.ID] = cur
	case EventToolInputEnd:
		cur, ok := state.ToolInputs[event.ID]
		if !ok {
			cur = ToolInputAssembly{Name: event.Name}
		}
		if event.ProviderMetadata != nil {
			cur.ProviderMetadata = event.ProviderMetadata
		}
		state.ToolInputs[event.ID] = cur
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

func (r *LLMResponse) ToolCalls() []LLMEvent {
	var calls []LLMEvent
	for _, e := range r.Events {
		if e.IsToolCall() {
			calls = append(calls, e)
		}
	}
	return calls
}

type PreparedRequest struct {
	ID       string
	Route    RouteID
	Protocol ProtocolID
	Model    ModelSchema
	Body     any
	Metadata map[string]any
}
