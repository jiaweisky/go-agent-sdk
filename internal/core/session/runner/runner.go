package runner

import (
	"context"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// RunInput describes the session to run.
type RunInput struct {
	SessionID schema.SessionID
	Force     bool
}

// RunError is the union of the runner's error types: llm.LLMError, ModelError,
// MessageDecodeError, ContextSnapshotDecodeError,
// SystemContext.InitializationBlocked and ToolOutputStore.StorageError.
type RunError = error

// SessionRunner runs one session until it settles.
type SessionRunner interface {
	// Run drains eligible durable work. An explicit run performs one provider
	// attempt even when no work is eligible. Cancelling ctx interrupts the
	// in-flight provider stream.
	Run(ctx context.Context, input RunInput) error
}
