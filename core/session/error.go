package session

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// MessageDecodeError reports a stored message that cannot be decoded.
type MessageDecodeError struct {
	SessionID schema.SessionID
	MessageID schema.MessageID
}

func (e *MessageDecodeError) Error() string {
	return fmt.Sprintf("Failed to decode message %s in session %s", e.MessageID, e.SessionID)
}

// ContextSnapshotDecodeError reports a stored context snapshot that cannot be
// decoded.
type ContextSnapshotDecodeError struct {
	SessionID schema.SessionID
	Details   string
}

func (e *ContextSnapshotDecodeError) Error() string {
	return fmt.Sprintf("Failed to decode context snapshot for session %s: %s", e.SessionID, e.Details)
}
