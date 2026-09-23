package runner

import (
	"context"

	"github.com/user/go-agent-sdk/schema"
)

// RunInput mirrors the SessionRunner.run argument.
type RunInput struct {
	SessionID schema.SessionID
	Force     bool
}

// RunError mirrors SessionRunner.RunError (the union of runner error types:
// llm.LLMError, ModelError, MessageDecodeError, ContextSnapshotDecodeError,
// SystemContext.InitializationBlocked, ToolOutputStore.StorageError).
type RunError = error

// SessionRunner mirrors the SessionRunner service interface.
type SessionRunner interface {
	// Run drains eligible durable work. An explicit run performs one provider
	// attempt even when no work is eligible. Cancelling ctx interrupts the
	// in-flight provider stream.
	Run(ctx context.Context, input RunInput) error
}
