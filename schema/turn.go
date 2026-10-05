package schema

// Turn state metadata keys. The owner distinguishes a turn maintained by the
// session runner (local execution) from one maintained by an inbound A2A task
// store, so the two never both claim a session's turn.
const (
	TurnMetaOwner  = "sdk.owner"
	TurnOwnerA2A   = "a2a"
	TurnOwnerLocal = "local"
)

// TurnID identifies one turn: a single, terminal-bound unit of work within a
// durable session. A turn is the persistence anchor for an A2A task (taskId)
// and for a subagent's delegated run.
type TurnID string

func (id TurnID) String() string {
	return string(id)
}

// NewTurnID generates a turn identifier.
func NewTurnID() TurnID {
	return TurnID(generateID("trn", true))
}

// ParseTurnID validates an existing turn id.
func ParseTurnID(id string) (TurnID, error) {
	if err := validateID("trn", id); err != nil {
		return "", err
	}
	return TurnID(id), nil
}

// TurnState is the lifecycle state of a turn.
type TurnState string

const (
	// TurnStateRunning means the turn is executing.
	TurnStateRunning TurnState = "running"
	// TurnStateAwaitingInput means the turn is parked waiting for a
	// permission/question reply (A2A input-required).
	TurnStateAwaitingInput TurnState = "awaiting_input"
	// TurnStateCompleted means the turn finished successfully.
	TurnStateCompleted TurnState = "completed"
	// TurnStateFailed means the turn ended in an error.
	TurnStateFailed TurnState = "failed"
	// TurnStateCanceled means the turn was interrupted.
	TurnStateCanceled TurnState = "canceled"
	// TurnStateRejected means the turn was refused before execution.
	TurnStateRejected TurnState = "rejected"
)

// Terminal reports whether the state is final.
func (s TurnState) Terminal() bool {
	switch s {
	case TurnStateCompleted, TurnStateFailed, TurnStateCanceled, TurnStateRejected:
		return true
	default:
		return false
	}
}

// TurnInfo is a durable record of one turn. It is the core entity shared by
// local subagent delegation and remote (A2A) task execution.
type TurnInfo struct {
	ID               TurnID         `json:"id"`
	SessionID        SessionID      `json:"sessionID"`
	ParentTurnID     *TurnID        `json:"parentTurnID,omitempty"`
	InputID          *MessageID     `json:"inputID,omitempty"`
	State            TurnState      `json:"state"`
	PendingRequestID *string        `json:"pendingRequestID,omitempty"`
	Agent            *AgentID       `json:"agent,omitempty"`
	Model            *ModelRef      `json:"model,omitempty"`
	AnchorSeq        *int64         `json:"anchorSeq,omitempty"`
	EndSeq           *int64         `json:"endSeq,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	TimeCreated      int64          `json:"timeCreated"`
	TimeUpdated      int64          `json:"timeUpdated"`
}
