package schema

type ProtocolID string

type RouteID string

type ModelID string

type ProviderID string

type ResponseID string

type ContentBlockID string

type ToolCallID string

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

type TextVerbosity string

const (
	TextVerbosityLow    TextVerbosity = "low"
	TextVerbosityMedium TextVerbosity = "medium"
	TextVerbosityHigh   TextVerbosity = "high"
)

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type FinishReason string

const (
	ReasonStop          FinishReason = "stop"
	ReasonLength        FinishReason = "length"
	ReasonToolCalls     FinishReason = "tool-calls"
	ReasonContentFilter FinishReason = "content-filter"
	ReasonError         FinishReason = "error"
	ReasonUnknown       FinishReason = "unknown"
)

type JsonSchema map[string]any

type ProviderMetadata map[string]map[string]any
