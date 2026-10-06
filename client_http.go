package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// ErrUnknownAgent is returned when an operation names an agent that is not
// registered.
var ErrUnknownAgent = errors.New("unknown agent")

// ErrSessionNotFound is returned when an operation names a session that does not
// exist.
var ErrSessionNotFound = errors.New("session not found")

// EventStream exposes the raw event bus for protocol adapters (the HTTP server,
// future transports). It is the raw counterpart to Client.On, which maps to the
// high-level Event union. Adapters use AllBounded for the live stream (every
// event, including live-only deltas and HITL) and Durable(aggregateID, after)
// for persisted replay. Mirrors Client.TurnStore.
func (c *Client) EventStream() core.Event { return c.rt.events }

// GetSession returns a session's stored info (including its current model)
// without attaching a handle. It errors with ErrSessionNotFound when absent.
func (c *Client) GetSession(ctx context.Context, sessionID string) (schema.SessionInfo, error) {
	info, err := c.rt.store.Get(schema.SessionID(sessionID))
	if err != nil {
		return schema.SessionInfo{}, err
	}
	if info == nil {
		return schema.SessionInfo{}, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return *info, nil
}

// waitPollInterval is how often WaitSession checks whether a session is idle.
const waitPollInterval = 50 * time.Millisecond

// WaitSession blocks until the session is idle or timeout elapses, and reports
// whether it is idle. A zero timeout waits until the context is cancelled. It
// errors with ErrSessionNotFound when the session does not exist.
func (c *Client) WaitSession(ctx context.Context, sessionID string, timeout time.Duration) (bool, error) {
	if _, err := c.GetSession(ctx, sessionID); err != nil {
		return false, err
	}
	id := schema.SessionID(sessionID)
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		if !c.rt.coord.IsActive(id) {
			return true, nil
		}
		if timeout > 0 && !time.Now().Before(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(waitPollInterval):
		}
	}
}
