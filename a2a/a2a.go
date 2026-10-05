// Package a2a contains protocol-agnostic helpers for the SDK's Agent2Agent
// (A2A) support: identity and metadata conventions shared by the inbound
// (server) and outbound (client) adapters. See docs/DESIGN-A2A.md.
package a2a

// Metadata prefixes. A2A has no standard metadata key space, so the SDK adopts
// the conventions used by ADK (adk_ for keys written to A2A objects) and
// reserves sdk_ for SDK-specific A2A keys and a2a: for keys written back into
// SDK session events.
const (
	// A2AMetaPrefix prefixes shared keys the SDK writes into A2A object metadata.
	A2AMetaPrefix = "adk_"
	// SDKMetaPrefix prefixes SDK-specific keys written into A2A object metadata.
	SDKMetaPrefix = "sdk_"
	// SessionKeyPrefix prefixes keys written into SDK session event metadata.
	SessionKeyPrefix = "a2a:"
)

// Shared metadata keys (aligned with ADK) written into A2A object metadata.
//
// This is the full vocabulary of docs/DESIGN-A2A.md §4.3. Some keys are not
// emitted yet; they are declared here so later phases never hand-write a
// metadata string.
const (
	MetaType          = "type"
	MetaThought       = "thought"
	MetaPartial       = "partial"
	MetaIsLongRunning = "is_long_running"
	MetaUsageMetadata = "usage_metadata"
	MetaErrorCode     = "error_code"
	MetaSessionID     = "session_id"
	MetaInvocationID  = "invocation_id"
	MetaAuthor        = "author"
	MetaBranch        = "branch"
	MetaAppName       = "app_name"
	MetaUserID        = "user_id"
	MetaTaskID        = "task_id"
	MetaContextID     = "context_id"
)

// Function-call type values used by ADK-compatible data parts.
const (
	TypeFunctionCall     = "function_call"
	TypeFunctionResponse = "function_response"
)

// SDK-specific metadata keys, reserved for the same vocabulary.
const (
	MetaFileDiff     = "file_diff"
	MetaToolProgress = "tool_progress"
)

// ToA2AMetaKey prefixes key with the shared A2A metadata prefix.
func ToA2AMetaKey(key string) string {
	return A2AMetaPrefix + key
}

// ToSDKObjectMetaKey prefixes key with the prefix used for SDK-specific keys
// written into A2A object metadata.
func ToSDKObjectMetaKey(key string) string {
	return SDKMetaPrefix + key
}

// ToSessionMetaKey prefixes key with the prefix used for key-value pairs the
// SDK writes into its own session event metadata. It is reserved for the
// inbound direction (A2A metadata read back into session events) and is not
// emitted yet.
func ToSessionMetaKey(key string) string {
	return SessionKeyPrefix + key
}
