package session

import (
	"fmt"

	"github.com/user/go-agent-sdk/schema"
)

// MessageDecodeError mirrors Session.MessageDecodeError.
type MessageDecodeError struct {
	SessionID schema.SessionID
	MessageID schema.MessageID
}

func (e *MessageDecodeError) Error() string {
	return fmt.Sprintf("Failed to decode message %s in session %s", e.MessageID, e.SessionID)
}

// ContextSnapshotDecodeError mirrors Session.ContextSnapshotDecodeError.
type ContextSnapshotDecodeError struct {
	SessionID schema.SessionID
	Details   string
}

func (e *ContextSnapshotDecodeError) Error() string {
	return fmt.Sprintf("Failed to decode context snapshot for session %s: %s", e.SessionID, e.Details)
}
