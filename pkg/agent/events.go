package agent

import (
	"encoding/json"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// PanicInfo describes a panic recovered from a user event handler.
type PanicInfo struct {
	SessionID string
	Value     any
}

// listenEvents subscribes handler to the high-level events mapped from every
// payload. Delivery is synchronous, like Session.On: a slow observer applies
// backpressure to the publishing turn rather than losing events. The returned
// function unsubscribes; a failed subscription yields a no-op.
func listenEvents(events core.Event, handler func(Event), report func(PanicInfo)) func() {
	if events == nil {
		return func() {}
	}
	unsubscribe, err := events.Listen(func(payload schema.EventPayload) error {
		mapped, count := mapEvents(payload)
		for i := 0; i < count; i++ {
			runHandler(handler, mapped[i], report, "")
		}
		return nil
	})
	if err != nil {
		return func() {}
	}
	return unsubscribe
}

// runHandler invokes a subscriber, recovering a panic so one bad handler cannot
// crash the turn goroutine or skip the remaining subscribers. A recovered panic
// is reported through report when set.
func runHandler(handler func(Event), event Event, report func(PanicInfo), sessionID string) {
	defer func() {
		if value := recover(); value != nil && report != nil {
			report(PanicInfo{SessionID: sessionID, Value: value})
		}
	}()
	handler(event)
}

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
type Event interface{ sessionID() string }

// TextDelta is a streamed assistant text chunk.
type TextDelta struct {
	SessionID string
	MessageID string
	TextID    string
	Delta     string
}

func (e TextDelta) sessionID() string { return e.SessionID }

// TextEnded is the completed assistant text for one text part.
type TextEnded struct {
	SessionID string
	MessageID string
	TextID    string
	Text      string
}

func (e TextEnded) sessionID() string { return e.SessionID }

// ReasoningDelta is a streamed reasoning chunk (model dependent).
type ReasoningDelta struct {
	SessionID   string
	MessageID   string
	ReasoningID string
	Delta       string
}

func (e ReasoningDelta) sessionID() string { return e.SessionID }

// ReasoningEnded is the completed reasoning text for one part.
type ReasoningEnded struct {
	SessionID   string
	MessageID   string
	ReasoningID string
	Text        string
}

func (e ReasoningEnded) sessionID() string { return e.SessionID }

// ToolCalled reports a tool invocation started by the model.
type ToolCalled struct {
	SessionID string
	MessageID string
	CallID    string
	Name      string
	Input     any
}

func (e ToolCalled) sessionID() string { return e.SessionID }

// ToolSuccess reports a completed tool invocation.
type ToolSuccess struct {
	SessionID string
	MessageID string
	CallID    string
	Result    any
	Content   any
	// Structured is the tool's structured output, e.g. the file diffs an
	// edit/write/apply_patch tool produced.
	Structured any
}

func (e ToolSuccess) sessionID() string { return e.SessionID }

// ToolFailed reports a failed tool invocation.
type ToolFailed struct {
	SessionID string
	MessageID string
	CallID    string
	Error     any
	Result    any
}

func (e ToolFailed) sessionID() string { return e.SessionID }

// ToolInputStarted reports that a tool call's input started streaming.
type ToolInputStarted struct {
	SessionID string
	MessageID string
	CallID    string
	Name      string
}

func (e ToolInputStarted) sessionID() string { return e.SessionID }

// ToolProgress reports a tool's incremental progress.
type ToolProgress struct {
	SessionID  string
	MessageID  string
	CallID     string
	Structured any
	Content    any
}

func (e ToolProgress) sessionID() string { return e.SessionID }

// ReasoningStarted reports the start of a reasoning part.
type ReasoningStarted struct {
	SessionID   string
	MessageID   string
	ReasoningID string
}

func (e ReasoningStarted) sessionID() string { return e.SessionID }

// Retried reports a provider retry.
//
// It has no producer: nothing publishes session.next.retried (V2 core does not
// emit it either), so subscribers never receive it. Kept for parity.
type Retried struct {
	SessionID string
	Attempt   any
	Error     any
}

func (e Retried) sessionID() string { return e.SessionID }

// SessionStatus reports a session's run status ("busy" or "idle"), so callers can
// animate a spinner only while a session is actually running.
type SessionStatus struct {
	SessionID string
	Status    string
}

func (e SessionStatus) sessionID() string { return e.SessionID }

// TitleUpdated reports that the session's title changed, so callers can refresh
// a session list without re-querying.
type TitleUpdated struct {
	SessionID string
	Title     string
}

func (e TitleUpdated) sessionID() string { return e.SessionID }

// TodoUpdated reports the session's full task list after the model calls the
// built-in todowrite tool. Each call replaces the whole list.
type TodoUpdated struct {
	SessionID string
	Todos     []schema.SessionTodoInfo
}

func (e TodoUpdated) sessionID() string { return e.SessionID }

// AttachmentOmitted reports an attachment the selected model could not carry, so
// it was replaced by a text note in the request. Reason is one of the
// schema.SessionAttachmentOmitReason values.
type AttachmentOmitted struct {
	SessionID string
	MessageID string
	Name      string
	MIME      string
	Reason    string
	// Detail is a human hint for the reason (e.g. the accepted size limit), so a
	// client can tell the caller how to fix it.
	Detail string
}

func (e AttachmentOmitted) sessionID() string { return e.SessionID }

// PermissionAsked reports a tool asking for permission. When the client holds
// interactions, the turn blocks until PermissionReplied or
// Client.Permission().Reply resolves the request.
type PermissionAsked struct {
	SessionID string
	Request   PermissionRequest
}

func (e PermissionAsked) sessionID() string { return e.SessionID }

// PermissionReplied reports that a permission request was resolved.
type PermissionReplied struct {
	SessionID string
	RequestID string
	Reply     string
}

func (e PermissionReplied) sessionID() string { return e.SessionID }

// QuestionAsked reports the "question" tool asking the user. When the client
// holds interactions, the turn blocks until QuestionReplied or
// Client.Question().Reply/Reject resolves the request.
type QuestionAsked struct {
	SessionID string
	Request   QuestionRequest
}

func (e QuestionAsked) sessionID() string { return e.SessionID }

// QuestionReplied reports that a question was answered.
type QuestionReplied struct {
	SessionID string
	RequestID string
	Answers   []schema.QuestionAnswer
}

func (e QuestionReplied) sessionID() string { return e.SessionID }

// QuestionRejected reports that a question was dismissed.
type QuestionRejected struct {
	SessionID string
	RequestID string
}

func (e QuestionRejected) sessionID() string { return e.SessionID }

// CompactionStarted reports that context compaction started.
type CompactionStarted struct {
	SessionID string
	MessageID string
	Reason    any
}

func (e CompactionStarted) sessionID() string { return e.SessionID }

// ShellStarted reports a shell command started.
//
// It has no producer: nothing publishes session.next.shell.started (V2 core
// does not emit it either), so subscribers never receive it. Kept for parity.
type ShellStarted struct {
	SessionID string
	MessageID string
	CallID    string
	Command   string
}

func (e ShellStarted) sessionID() string { return e.SessionID }

// ShellEnded reports a shell command finished.
//
// It has no producer: nothing publishes session.next.shell.ended (V2 core does
// not emit it either), so subscribers never receive it. Kept for parity.
type ShellEnded struct {
	SessionID string
	CallID    string
	Output    any
}

func (e ShellEnded) sessionID() string { return e.SessionID }

// StepStarted reports the start of an agent step.
type StepStarted struct {
	SessionID string
	MessageID string
	Agent     string
}

func (e StepStarted) sessionID() string { return e.SessionID }

// StepEnded reports the end of an agent step.
type StepEnded struct {
	SessionID string
	MessageID string
	Finish    any
	// Files lists the project-relative files the step changed.
	Files []schema.RelativePath
}

func (e StepEnded) sessionID() string { return e.SessionID }

// StepFailed reports a failed agent step.
type StepFailed struct {
	SessionID string
	MessageID string
	Error     any
}

func (e StepFailed) sessionID() string { return e.SessionID }

// CompactionEnded reports that context compaction finished.
type CompactionEnded struct {
	SessionID string
	MessageID string
	Text      string
}

func (e CompactionEnded) sessionID() string { return e.SessionID }

// ErrorEvent reports a session-level failure.
type ErrorEvent struct {
	SessionID string
	Err       error
}

func (e ErrorEvent) sessionID() string { return e.SessionID }

// Usage carries the provider's original usage payload for one turn, verbatim
// (keyed by provider name), so provider-specific billing buckets survive. The
// SDK does not parse, normalize, or total it: subscribe via
// Client.On/Session.On and interpret the provider's own fields yourself.
type Usage struct {
	SessionID        string
	ProviderMetadata map[string]map[string]any
}

func (e Usage) sessionID() string { return e.SessionID }

// Idle is a synthetic event emitted by Session.Send when the turn settles.
type Idle struct {
	SessionID string
}

func (e Idle) sessionID() string { return e.SessionID }

// mapEvent converts a core session event payload into a high-level Event.
// The bool is false for event types the high-level API does not surface.
func mapEvent(payload schema.EventPayload) (Event, bool) {
	if payload.Type == schema.SessionTodoEventUpdated.Type {
		return mapTodoEvent(payload.Data), true
	}
	if event, ok := mapInteractionEvent(payload); ok {
		return event, true
	}
	data, _ := payload.Data.(map[string]any)
	sessionID := fieldString(data, "sessionID")
	messageID := fieldString(data, "assistantMessageID")
	if event, ok := mapContentEvent(payload.Type, data, sessionID, messageID); ok {
		return event, true
	}
	if event, ok := mapToolEvent(payload.Type, data, sessionID, messageID); ok {
		return event, true
	}
	return mapRunEvent(payload.Type, data, sessionID, messageID)
}

// mapTodoEvent decodes a todo.updated payload. Its publisher sends a struct
// rather than a map, so this round-trips through JSON instead of reading map
// fields like the other events.
func mapTodoEvent(data any) TodoUpdated {
	var decoded struct {
		SessionID string                   `json:"sessionID"`
		Todos     []schema.SessionTodoInfo `json:"todos"`
	}
	if encoded, err := json.Marshal(data); err == nil {
		_ = json.Unmarshal(encoded, &decoded)
	}
	return TodoUpdated{SessionID: decoded.SessionID, Todos: decoded.Todos}
}

// mapInteractionEvent maps permission and question lifecycle events. Their data
// is a published struct or a decoded map, so each is JSON round-tripped.
func mapInteractionEvent(payload schema.EventPayload) (Event, bool) {
	if event, ok := mapPermissionEvent(payload); ok {
		return event, true
	}
	return mapQuestionEvent(payload)
}

// permissionRepliedData is the permission.v2.replied payload.
type permissionRepliedData struct {
	SessionID string `json:"sessionID"`
	RequestID string `json:"requestID"`
	Reply     string `json:"reply"`
}

// mapPermissionEvent maps permission.v2.asked and .replied.
func mapPermissionEvent(payload schema.EventPayload) (Event, bool) {
	switch payload.Type {
	case schema.PermissionEventAsked.Type:
		request, ok := decodeEvent[schema.PermissionRequest](payload)
		if !ok {
			return nil, false
		}
		return PermissionAsked{
			SessionID: request.SessionID, Request: permissionRequestsFrom([]core.PermissionRequest{request})[0],
		}, true
	case schema.PermissionEventReplied.Type:
		data, ok := decodeEvent[permissionRepliedData](payload)
		if !ok {
			return nil, false
		}
		return PermissionReplied(data), true
	}
	return nil, false
}

// questionRepliedData is the question.v2.replied payload.
type questionRepliedData struct {
	SessionID string                  `json:"sessionID"`
	RequestID string                  `json:"requestID"`
	Answers   []schema.QuestionAnswer `json:"answers"`
}

// questionRejectedData is the question.v2.rejected payload.
type questionRejectedData struct {
	SessionID string `json:"sessionID"`
	RequestID string `json:"requestID"`
}

// mapQuestionEvent maps question.v2.asked, .replied and .rejected.
func mapQuestionEvent(payload schema.EventPayload) (Event, bool) {
	switch payload.Type {
	case schema.QuestionEventAsked.Type:
		request, ok := decodeEvent[schema.QuestionRequest](payload)
		if !ok {
			return nil, false
		}
		return QuestionAsked{
			SessionID: request.SessionID, Request: questionRequestsFrom([]core.QuestionRequest{request})[0],
		}, true
	case schema.QuestionEventReplied.Type:
		data, ok := decodeEvent[questionRepliedData](payload)
		if !ok {
			return nil, false
		}
		return QuestionReplied(data), true
	case schema.QuestionEventRejected.Type:
		data, ok := decodeEvent[questionRejectedData](payload)
		if !ok {
			return nil, false
		}
		return QuestionRejected(data), true
	}
	return nil, false
}

// decodeEvent JSON-round-trips a payload's data into T, since a publisher sends
// either a typed struct or a decoded map.
func decodeEvent[T any](payload schema.EventPayload) (T, bool) {
	var data T
	encoded, err := json.Marshal(payload.Data)
	if err != nil {
		return data, false
	}
	if err := json.Unmarshal(encoded, &data); err != nil {
		return data, false
	}
	return data, true
}

// mapContentEvent maps assistant text and reasoning streaming events.
func mapContentEvent(eventType string, data map[string]any, sessionID, messageID string) (Event, bool) {
	switch eventType {
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
	case schema.SessionEventReasoningStarted.Type:
		return ReasoningStarted{
			SessionID: sessionID, MessageID: messageID, ReasoningID: fieldString(data, "reasoningID"),
		}, true
	}
	return nil, false
}

// mapToolEvent maps tool invocation and shell events.
func mapToolEvent(eventType string, data map[string]any, sessionID, messageID string) (Event, bool) {
	switch eventType {
	case schema.SessionEventToolCalled.Type:
		return ToolCalled{
			SessionID: sessionID, MessageID: messageID, CallID: fieldString(data, "callID"),
			Name: fieldString(data, "tool"), Input: data["input"],
		}, true
	case schema.SessionEventToolSuccess.Type:
		return ToolSuccess{
			SessionID: sessionID, MessageID: messageID, CallID: fieldString(data, "callID"),
			Result: data["result"], Content: data["content"], Structured: data["structured"],
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
	case schema.SessionEventShellStarted.Type:
		return ShellStarted{
			SessionID: sessionID, MessageID: messageID,
			CallID: fieldString(data, "callID"), Command: fieldString(data, "command"),
		}, true
	case schema.SessionEventShellEnded.Type:
		return ShellEnded{SessionID: sessionID, CallID: fieldString(data, "callID"), Output: data["output"]}, true
	}
	return nil, false
}

// mapRunEvent maps step, compaction, retry and session-status events.
func mapRunEvent(eventType string, data map[string]any, sessionID, messageID string) (Event, bool) {
	switch eventType {
	case schema.SessionEventRetried.Type:
		return Retried{SessionID: sessionID, Attempt: data["attempt"], Error: data["error"]}, true
	case schema.SessionStatusEventStatus.Type:
		return SessionStatus{SessionID: sessionID, Status: sessionStatusType(data["status"])}, true
	case schema.SessionEventTitleUpdated.Type:
		return TitleUpdated{SessionID: sessionID, Title: fieldString(data, "title")}, true
	case schema.SessionEventCompactionStarted.Type:
		return CompactionStarted{
			SessionID: sessionID,
			MessageID: fieldString(data, "messageID"),
			Reason:    data["reason"],
		}, true
	case schema.SessionEventStepStarted.Type:
		return StepStarted{
			SessionID: sessionID, MessageID: messageID, Agent: fieldString(data, "agent"),
		}, true
	case schema.SessionEventStepEnded.Type:
		return StepEnded{
			SessionID: sessionID, MessageID: messageID,
			Finish: data["finish"], Files: relativePathsFrom(data["files"]),
		}, true
	case schema.SessionEventStepFailed.Type:
		return StepFailed{
			SessionID: sessionID, MessageID: messageID, Error: data["error"],
		}, true
	case schema.SessionEventCompactionEnded.Type:
		return CompactionEnded{
			SessionID: sessionID, MessageID: fieldString(data, "messageID"), Text: fieldString(data, "text"),
		}, true
	case schema.SessionEventAttachmentOmitted.Type:
		return AttachmentOmitted{
			SessionID: sessionID,
			MessageID: fieldString(data, "messageID"),
			Name:      fieldString(data, "name"),
			MIME:      fieldString(data, "mime"),
			Reason:    fieldString(data, "reason"),
			Detail:    fieldString(data, "detail"),
		}, true
	}
	return nil, false
}

// maxEventsPerPayload bounds the events one payload can map to.
const maxEventsPerPayload = 2

// mapEvents converts a payload into every high-level event it carries. Most
// payloads map to one event; step.ended also yields a Usage event. It returns a
// fixed array and count so the per-event hot path allocates nothing.
func mapEvents(payload schema.EventPayload) ([maxEventsPerPayload]Event, int) {
	event, ok := mapEvent(payload)
	if !ok {
		return [maxEventsPerPayload]Event{}, 0
	}
	if payload.Type != schema.SessionEventStepEnded.Type {
		return [maxEventsPerPayload]Event{event}, 1
	}
	data, _ := payload.Data.(map[string]any)
	return [maxEventsPerPayload]Event{event, Usage{
		SessionID:        fieldString(data, "sessionID"),
		ProviderMetadata: usageProviderMetadataFrom(data["tokens"]),
	}}, 2
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

// fieldString reads a string-ish field from a decoded event payload, rendering
// non-strings with fmt.
// relativePathsFrom decodes a files list that is []RelativePath for a live
// event or []any of strings when replayed from JSON.
func relativePathsFrom(value any) []schema.RelativePath {
	if paths, ok := value.([]schema.RelativePath); ok {
		return paths
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var paths []schema.RelativePath
	if err := json.Unmarshal(encoded, &paths); err != nil {
		return nil
	}
	return paths
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
