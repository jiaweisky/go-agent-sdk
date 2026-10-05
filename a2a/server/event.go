package server

import (
	"context"
	"iter"

	"github.com/user/go-agent-sdk/schema"
)

// EventKind classifies a session event the A2A executor maps. The zero value
// is EventUnknown so an uninitialized event is never mapped as output.
type EventKind int

const (
	// EventUnknown is the zero value: no event.
	EventUnknown EventKind = iota
	// EventText is an assistant text delta.
	EventText
	// EventReasoning is an assistant reasoning delta.
	EventReasoning
	// EventToolCalled reports a tool invocation started.
	EventToolCalled
	// EventToolResult reports a completed tool invocation (success or failure).
	EventToolResult
	// EventToolProgress reports incremental tool progress.
	EventToolProgress
	// EventStepStarted reports the start of an agent step (used as a heartbeat).
	EventStepStarted
	// EventUsage reports the provider's token usage for a completed step.
	EventUsage
	// EventAwaitInput reports that the turn is waiting for user input
	// (permission or question) and carries the pending request id.
	EventAwaitInput
	// EventError reports a session-level failure.
	EventError
	// EventIdle marks the end of a turn.
	EventIdle
)

// Event is a session event presented to the A2A executor. It is intentionally
// narrow: it carries only what maps onto A2A, keeping the server package free
// of the SDK's full event union.
type Event struct {
	Kind EventKind

	// Text is set for EventText and EventReasoning.
	Text string

	// Tool fields (EventToolCalled, EventToolResult, EventToolProgress).
	ToolName string
	CallID   string
	Input    any
	Result   any
	// Structured is the tool's structured output (for example the file diffs an
	// edit or apply_patch tool produced), set on EventToolResult.
	Structured any
	IsError    bool

	// Usage is the provider's token usage for a step, set on EventUsage.
	Usage any

	// RequestID is set for EventAwaitInput: the pending permission or question
	// request id.
	RequestID string
	// IsQuestion is set for EventAwaitInput when the pending request is a
	// question (false means a permission request).
	IsQuestion bool
	// Questions carries the questions a question request asked, so they can be
	// forwarded to the A2A client.
	Questions []schema.QuestionInfo

	// Err is set for EventError.
	Err error
}

// Answer is one question answer.
type Answer struct {
	Question string
	Labels   []string
}

// File is an inbound attachment the executor asks the session to materialize.
type File struct {
	// Name is the suggested file name.
	Name string
	// MIME is the media type.
	MIME string
	// Data is the raw content, when the part carried bytes.
	Data []byte
	// URI is a remote reference, when the part carried a URL (not fetched here).
	URI string
}

// SendRequest is one inbound turn's input.
type SendRequest struct {
	// Prompt is the message text (plus any structured data rendering).
	Prompt string
	// Files are attachments to write into the workspace before the turn.
	Files []File
}

// Session is the SDK session surface the A2A executor drives. Turns are keyed
// by the A2A task id, so a parked turn and a later turn on the same session do
// not share state.
type Session interface {
	// ID returns the durable session id.
	ID() string
	// Send admits a request for the turn identified by turnID and streams the
	// resulting turn's events, ending with EventIdle. Iterating the sequence is
	// what drives the turn.
	Send(ctx context.Context, turnID string, req SendRequest) iter.Seq2[Event, error]
	// Resume continues streaming the events of the turn identified by turnID
	// after it was parked at EventAwaitInput and the reply has been delivered.
	Resume(ctx context.Context, turnID string) iter.Seq2[Event, error]
	// Steer feeds a message into the session's active turn at its next step
	// boundary, without starting a new turn. It is used when a message arrives
	// for a task that is still working.
	Steer(prompt string) error
	// ReplyPermission resolves a pending permission request.
	ReplyPermission(requestID, reply string) error
	// ReplyQuestion resolves a pending question request.
	ReplyQuestion(requestID string, answers []Answer) error
	// RejectQuestion dismisses a pending question request.
	RejectQuestion(requestID string) error
	// Abort interrupts the session's active turn.
	Abort()
	// OnTurnEnd registers a callback invoked once the turn identified by turnID
	// has fully settled (its event stream closed). It lets the executor release
	// a session after a cancel, when no terminal event is otherwise emitted.
	OnTurnEnd(turnID string, fn func())
	// Close releases the session handle.
	Close() error
}

// SessionRequest describes the session an inbound task needs.
type SessionRequest struct {
	// AgentID is the exposed agent to bind the session to.
	AgentID string
	// Workspace is the resolved execution context for the call.
	Workspace Workspace
}

// Factory creates and resumes sessions for exposed agents.
type Factory interface {
	// CreateSession creates a new root session for the request.
	CreateSession(ctx context.Context, req SessionRequest) (Session, error)
	// ResumeSession reattaches to an existing session, applying the resolved
	// workspace (its permissions) for this call.
	ResumeSession(ctx context.Context, sessionID string, workspace Workspace) (Session, error)
}

// ArtifactExternalizer lets a host move artifact content out of line (for
// example to object storage) or rewrite it. It receives the task id and the
// emitted artifact (an *a2a.Artifact as any, because a2a-go types never appear
// in the SDK's public API) and may return a replacement artifact or an error.
type ArtifactExternalizer func(ctx context.Context, taskID string, artifact any) (any, error)
