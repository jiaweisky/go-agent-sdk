// Package client defines the SDK's thin, protocol-agnostic surface for calling
// a remote A2A agent. The concrete implementation over a2a-go lives in
// a2a/a2ago so the dependency can be swapped without touching callers.
package client

import (
	"context"
	"iter"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// Card is the subset of a remote agent's AgentCard the SDK consumes.
type Card struct {
	// Name is the agent's human-readable name.
	Name string
	// Description describes the agent's purpose.
	Description string
	// Endpoint is the resolved JSON-RPC URL.
	Endpoint string
	// Streaming reports whether the remote supports streaming responses.
	Streaming bool
	// ProtocolVersion is the remote's A2A protocol version.
	ProtocolVersion string
}

// EventKind classifies an event received from a remote A2A agent. The zero
// value is EventUnknown so an uninitialized event is never mistaken for real
// output.
type EventKind int

const (
	// EventUnknown is the zero value: no event.
	EventUnknown EventKind = iota
	// EventArtifact is streamed assistant text.
	EventArtifact
	// EventReasoning is streamed assistant reasoning.
	EventReasoning
	// EventToolCall reports a remote tool invocation.
	EventToolCall
	// EventToolResult reports a remote tool result.
	EventToolResult
	// EventFileDiff reports a code change (an sdk_type=file_diff data part).
	EventFileDiff
	// EventStatus reports a task state change.
	EventStatus
	// EventInputRequired reports that the remote task is waiting for input.
	EventInputRequired
	// EventCompleted reports that the remote task finished successfully.
	EventCompleted
	// EventFailed reports that the remote task failed.
	EventFailed
	// EventCanceled reports that the remote task was canceled.
	EventCanceled
)

// Event is an event received from a remote A2A agent.
type Event struct {
	Kind      EventKind
	TaskID    string
	ContextID string
	// State is the A2A task state string for EventStatus.
	State string
	// Text is set for EventArtifact and EventReasoning.
	Text string
	// Tool fields.
	ToolName string
	CallID   string
	Input    any
	Result   any
	IsError  bool
	// RequestID is set for EventInputRequired.
	RequestID string
	// Questions carries the questions a remote agent asked, when it encodes
	// them for a human-in-the-loop park.
	Questions []schema.QuestionInfo
}

// Request is one outbound send.
type Request struct {
	// Prompt is the text sent to the remote agent.
	Prompt string
	// TaskID continues an existing remote task when non-empty.
	TaskID string
	// ContextID groups related tasks when non-empty.
	ContextID string
	// Response, when set, answers the function call a remote task parked at
	// input-required, so an ADK server can match the reply by call id.
	Response *Response
	// Files are attachments sent as A2A file parts.
	Files []FileAttachment
}

// FileAttachment is a file sent with a message. When Data is set it is sent as
// a raw part; otherwise URI is sent as a file reference.
type FileAttachment struct {
	MIME string
	Name string
	Data []byte
	URI  string
}

// Response answers a park. CallID and Name echo the remote function_call; Text
// is the user's answer, also sent as a plain text part.
type Response struct {
	CallID string
	Name   string
	Text   string
}

// Client calls a remote A2A agent.
type Client interface {
	// Card returns the resolved remote card.
	Card() Card
	// Send sends a message and streams the remote events until a terminal
	// state (or input-required).
	Send(ctx context.Context, req Request) iter.Seq2[Event, error]
	// Cancel requests cooperative cancellation of a remote task.
	Cancel(ctx context.Context, taskID string) error
	// Close releases the underlying transport.
	Close() error
}
