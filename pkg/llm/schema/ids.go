package schema

// ProtocolID names a wire protocol, such as "openai-responses".
type ProtocolID string

// RouteID names a configured route.
type RouteID string

// ModelID names a model.
type ModelID string

// ProviderID names a provider.
type ProviderID string

// ResponseID identifies one response.
type ResponseID string

// ContentBlockID identifies a content block within a response.
type ContentBlockID string

// ToolCallID identifies one tool call.
type ToolCallID string

// ReasoningEffort is how much reasoning a model should spend.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	ReasoningEffortMax     ReasoningEffort = "max"
)

// TextVerbosity is how verbose a model's text output should be.
type TextVerbosity string

const (
	TextVerbosityLow    TextVerbosity = "low"
	TextVerbosityMedium TextVerbosity = "medium"
	TextVerbosityHigh   TextVerbosity = "high"
)

// MessageRole is the part a message plays in the conversation.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// FinishReason is why a step or a response ended.
type FinishReason string

const (
	ReasonStop          FinishReason = "stop"
	ReasonLength        FinishReason = "length"
	ReasonToolCalls     FinishReason = "tool-calls"
	ReasonContentFilter FinishReason = "content-filter"
	ReasonError         FinishReason = "error"
	ReasonUnknown       FinishReason = "unknown"
)

// JsonSchema is a JSON schema document.
type JsonSchema map[string]any

// ProviderMetadata carries provider-specific values keyed by provider.
type ProviderMetadata map[string]map[string]any
