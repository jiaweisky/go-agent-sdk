// Package execution ports core/session/execution/local.ts: current-process
// routing for implicit-local Locations.
package execution

import (
	"context"
	"fmt"

	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/core/session/runner"
	"github.com/user/go-agent-sdk/schema"
)

// Local mirrors SessionExecutionLocal. The TS layer resolves the Location's
// runner through LocationServiceMap; the Go SDK injects the runner directly
// (narrow input).
type Local struct {
	store       *session.Store
	runner      runner.SessionRunner
	coordinator *session.Coordinator[schema.SessionID]
}

var _ session.Execution = (*Local)(nil)

func NewLocal(store *session.Store, sessionRunner runner.SessionRunner) *Local {
	local := &Local{store: store, runner: sessionRunner}
	local.coordinator = session.NewCoordinator(func(_ context.Context, sessionID schema.SessionID, force bool) error {
		stored, err := store.Get(sessionID)
		if err != nil {
			return err
		}
		if stored == nil {
			return fmt.Errorf("Session not found: %s", sessionID)
		}
		return sessionRunner.Run(runner.RunInput{SessionID: sessionID, Force: force})
	})
	return local
}

func (l *Local) Active() []schema.SessionID { return l.coordinator.Active() }

func (l *Local) Resume(sessionID schema.SessionID) error {
	return l.coordinator.Run(context.Background(), sessionID)
}

func (l *Local) Wake(sessionID schema.SessionID) { l.coordinator.Wake(sessionID) }

func (l *Local) Interrupt(sessionID schema.SessionID) { l.coordinator.Interrupt(sessionID) }
