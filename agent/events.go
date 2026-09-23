package agent

import (
	"encoding/json"
	"fmt"

	"github.com/user/go-agent-sdk/schema"
)

// Event is a high-level session event surfaced by Session.Send and Session.On.
//
// Events are a closed set; use a type switch to handle them:
//
//	switch e := event.(type) {
//	case agent.TextDelta:
//	    fmt.Print(e.Delta)
//	case agent.ToolCalled:
//	    fmt.Println("calling", e.Name)
//	case agent.Idle:
//	    // turn finished
//	}
type Event interface{ eventKind() string }

// TextDelta is a streamed assistant text chunk.
type TextDelta struct {
	SessionID string
	MessageID string
	TextID    string
	Delta     string
}

func (TextDelta) eventKind() string { return "text-delta" }

// TextEnded is the completed assistant text for one text part.
type TextEnded struct {
	SessionID string
	MessageID string
	TextID    string
	Text      string
}

func (TextEnded) eventKind() string { return "text-ended" }

// ReasoningDelta is a streamed reasoning chunk (model dependent).
type ReasoningDelta struct {
	SessionID   string
	MessageID   string
	ReasoningID string
	Delta       string
}

func (ReasoningDelta) eventKind() string { return "reasoning-delta" }

// ReasoningEnded is the completed reasoning text for one part.
type ReasoningEnded struct {
	SessionID   string
	MessageID   string
	ReasoningID string
	Text        string
}

func (ReasoningEnded) eventKind() string { return "reasoning-ended" }

// ToolCalled reports a tool invocation started by the model.
type ToolCalled struct {
	SessionID string
	MessageID string
	CallID    string
	Name      string
	Input     any
}

func (ToolCalled) eventKind() string { return "tool-called" }

// ToolSuccess reports a completed tool invocation.
type ToolSuccess struct {
	SessionID string
	MessageID string
	CallID    string
	Result    any
	Content   any
}

func (ToolSuccess) eventKind() string { return "tool-success" }

// ToolFailed reports a failed tool invocation.
type ToolFailed struct {
	SessionID string
	MessageID string
	CallID    string
	Error     any
	Result    any
}

func (ToolFailed) eventKind() string { return "tool-failed" }

// ToolInputStarted reports that a tool call's input started streaming.
type ToolInputStarted struct {
	SessionID string
	MessageID string
	CallID    string
	Name      string
}

func (ToolInputStarted) eventKind() string { return "tool-input-started" }

// ToolProgress reports a tool's incremental progress.
type ToolProgress struct {
	SessionID  string
	MessageID  string
	CallID     string
	Structured any
	Content    any
}

func (ToolProgress) eventKind() string { return "tool-progress" }

// ReasoningStarted reports the start of a reasoning part.
type ReasoningStarted struct {
	SessionID   string
	MessageID   string
	ReasoningID string
}

func (ReasoningStarted) eventKind() string { return "reasoning-started" }

// Retried reports a provider retry.
type Retried struct {
	SessionID string
	Attempt   any
	Error     any
}

func (Retried) eventKind() string { return "retried" }

// SessionStatus reports a session's run status ("busy" or "idle"). It mirrors
// the TS session.status event and lets callers animate a spinner only while a
// session is actually running.
type SessionStatus struct {
	SessionID string
	Status    string
}

func (SessionStatus) eventKind() string { return "session-status" }

// CompactionStarted reports that context compaction started.
type CompactionStarted struct {
	SessionID string
	MessageID string
	Reason    any
}

func (CompactionStarted) eventKind() string { return "compaction-started" }

// ShellStarted reports a shell command started.
type ShellStarted struct {
	SessionID string
	MessageID string
	CallID    string
	Command   string
}

func (ShellStarted) eventKind() string { return "shell-started" }

// ShellEnded reports a shell command finished.
type ShellEnded struct {
	SessionID string
	CallID    string
	Output    any
}

func (ShellEnded) eventKind() string { return "shell-ended" }

// StepStarted reports the start of an agent step.
type StepStarted struct {
	SessionID string
	MessageID string
	Agent     string
}

func (StepStarted) eventKind() string { return "step-started" }

// StepEnded reports the end of an agent step.
type StepEnded struct {
	SessionID string
	MessageID string
	Finish    any
}

func (StepEnded) eventKind() string { return "step-ended" }

// Usage carries the provider's original usage payload for one turn, verbatim
// (keyed by provider name), so provider-specific billing buckets survive. The
// SDK does not parse, normalize, or total it: subscribe via
// Client.On/Session.On and interpret the provider's own fields yourself.
type Usage struct {
	SessionID        string
	ProviderMetadata map[string]map[string]any
}

func (Usage) eventKind() string { return "usage" }

// StepFailed reports a failed agent step.
type StepFailed struct {
	SessionID string
	MessageID string
	Error     any
}

func (StepFailed) eventKind() string { return "step-failed" }

// CompactionEnded reports that context compaction finished.
type CompactionEnded struct {
	SessionID string
	MessageID string
	Text      string
}

func (CompactionEnded) eventKind() string { return "compaction-ended" }

// ErrorEvent reports a session-level failure.
type ErrorEvent struct {
	SessionID string
	Err       error
}

func (ErrorEvent) eventKind() string { return "error" }

// Idle is a synthetic event emitted by Session.Send when the turn settles.
type Idle struct {
	SessionID string
}

func (Idle) eventKind() string { return "idle" }

// mapEvent converts a core session event payload into a high-level Event.
// The bool is false for event types the high-level API does not surface.
func mapEvent(payload schema.EventPayload) (Event, bool) {
	data, _ := payload.Data.(map[string]any)
	sessionID := fieldString(data, "sessionID")
	messageID := fieldString(data, "assistantMessageID")
	switch payload.Type {
	case schema.SessionEventTextDelta.Type:
		return TextDelta{
			SessionID: sessionID, MessageID: messageID,
			TextID: fieldString(data, "textID"), Delta: fieldString(data, "delta"),
		}, true
	case schema.SessionEventTextEnded.Type:
		return TextEnded{
			SessionID: sessionID, MessageID: messageID,
			TextID: fieldString(data, "textID"), Text: fieldString(data, "text"),
		}, true
	case schema.SessionEventReasoningDelta.Type:
		return ReasoningDelta{
			SessionID: sessionID, MessageID: messageID,
			ReasoningID: fieldString(data, "reasoningID"), Delta: fieldString(data, "delta"),
		}, true
	case schema.SessionEventReasoningEnded.Type:
		return ReasoningEnded{
			SessionID: sessionID, MessageID: messageID,
			ReasoningID: fieldString(data, "reasoningID"), Text: fieldString(data, "text"),
		}, true
	case schema.SessionEventToolCalled.Type:
		return ToolCalled{
			SessionID: sessionID, MessageID: messageID, CallID: fieldString(data, "callID"),
			Name: fieldString(data, "tool"), Input: data["input"],
		}, true
	case schema.SessionEventToolSuccess.Type:
		return ToolSuccess{
			SessionID: sessionID, MessageID: messageID, CallID: fieldString(data, "callID"),
			Result: data["result"], Content: data["content"],
		}, true
	case schema.SessionEventToolFailed.Type:
		return ToolFailed{
			SessionID: sessionID, MessageID: messageID, CallID: fieldString(data, "callID"),
			Error: data["error"], Result: data["result"],
		}, true
	case schema.SessionEventToolInputStarted.Type:
		return ToolInputStarted{
			SessionID: sessionID, MessageID: messageID,
			CallID: fieldString(data, "callID"), Name: fieldString(data, "name"),
		}, true
	case schema.SessionEventToolProgress.Type:
		return ToolProgress{
			SessionID: sessionID, MessageID: messageID, CallID: fieldString(data, "callID"),
			Structured: data["structured"], Content: data["content"],
		}, true
	case schema.SessionEventReasoningStarted.Type:
		return ReasoningStarted{
			SessionID: sessionID, MessageID: messageID, ReasoningID: fieldString(data, "reasoningID"),
		}, true
	case schema.SessionEventRetried.Type:
		return Retried{SessionID: sessionID, Attempt: data["attempt"], Error: data["error"]}, true
	case schema.SessionStatusEventStatus.Type:
		return SessionStatus{SessionID: sessionID, Status: sessionStatusType(data["status"])}, true
	case schema.SessionEventCompactionStarted.Type:
		return CompactionStarted{SessionID: sessionID, MessageID: fieldString(data, "messageID"), Reason: data["reason"]}, true
	case schema.SessionEventShellStarted.Type:
		return ShellStarted{
			SessionID: sessionID, MessageID: messageID,
			CallID: fieldString(data, "callID"), Command: fieldString(data, "command"),
		}, true
	case schema.SessionEventShellEnded.Type:
		return ShellEnded{SessionID: sessionID, CallID: fieldString(data, "callID"), Output: data["output"]}, true
	case schema.SessionEventStepStarted.Type:
		return StepStarted{
			SessionID: sessionID, MessageID: messageID, Agent: fieldString(data, "agent"),
		}, true
	case schema.SessionEventStepEnded.Type:
		return StepEnded{
			SessionID: sessionID, MessageID: messageID,
			Finish: data["finish"],
		}, true
	case schema.SessionEventStepFailed.Type:
		return StepFailed{
			SessionID: sessionID, MessageID: messageID, Error: data["error"],
		}, true
	case schema.SessionEventCompactionEnded.Type:
		return CompactionEnded{
			SessionID: sessionID, MessageID: fieldString(data, "messageID"), Text: fieldString(data, "text"),
		}, true
	}
	return nil, false
}

// mapEvents converts a payload into every high-level event it carries. Most
// payloads map to one event; step.ended also yields a Usage event.
func mapEvents(payload schema.EventPayload) []Event {
	event, ok := mapEvent(payload)
	if !ok {
		return nil
	}
	events := []Event{event}
	if payload.Type == schema.SessionEventStepEnded.Type {
		data, _ := payload.Data.(map[string]any)
		events = append(events, Usage{
			SessionID:        fieldString(data, "sessionID"),
			ProviderMetadata: usageProviderMetadataFrom(data["tokens"]),
		})
	}
	return events
}

// usageProviderMetadataFrom extracts the provider's original usage payload from
// a step's tokens value (a runner.LLMEventTokens struct or a JSON map).
func usageProviderMetadataFrom(value any) map[string]map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded struct {
		ProviderMetadata map[string]map[string]any `json:"providerMetadata"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil
	}
	return decoded.ProviderMetadata
}

func fieldString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	switch value := data[key].(type) {
	case nil:
		return ""
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return fmt.Sprint(value)
	}
}

// sessionStatusType reads the "type" of a session.status payload, which is the
// tagged SessionStatusInfo union (or a plain map after JSON round-trips).
func sessionStatusType(value any) string {
	switch v := value.(type) {
	case schema.SessionStatusInfo:
		return v.SessionStatusInfoType()
	case map[string]any:
		if t, ok := v["type"].(string); ok {
			return t
		}
	}
	return ""
}
