package session

import "github.com/user/go-agent-sdk/schema"

// Execution mirrors SessionExecution.Interface: it routes execution from a
// Session ID to the runner owned by that Session's Location.
type Execution interface {
	// Active snapshots the Session IDs with an active execution owned by this
	// process.
	Active() []schema.SessionID
	// Resume starts execution while idle or joins the active execution.
	Resume(sessionID schema.SessionID) error
	// Wake registers newly recorded work. Repeated wakeups may coalesce.
	Wake(sessionID schema.SessionID)
	// Interrupt stops active work owned by this process. Idle interruption is a
	// no-op.
	Interrupt(sessionID schema.SessionID)
}

// NoopExecution mirrors SessionExecution.noopLayer.
type NoopExecution struct{}

func (NoopExecution) Active() []schema.SessionID    { return nil }
func (NoopExecution) Resume(schema.SessionID) error { return nil }
func (NoopExecution) Wake(schema.SessionID)         {}
func (NoopExecution) Interrupt(schema.SessionID)    {}
