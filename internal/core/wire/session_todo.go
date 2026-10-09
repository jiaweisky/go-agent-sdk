// Package wire connects the public core event service to the internal session
// engine. It exists so the public core package does not expose internal session
// or database types.
package wire

import (
	core "github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/internal/core/session"
)

// NewSessionTodo wires the core event service into session.Todo.
func NewSessionTodo(db *database.Database, events core.Event) *session.Todo {
	return session.NewTodo(db, sessionEventPublisher{events: events})
}

type sessionEventPublisher struct{ events core.Event }

func (p sessionEventPublisher) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	payload, err := p.events.Publish(def, data, nil)
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}

// PublishWithCommit satisfies session.ContextEpochPublisher: the commit hook
// runs inside the durable event transaction and receives its handle.
func (p sessionEventPublisher) PublishWithCommit(
	def schema.EventDefinition,
	data any,
	commit func(tx session.DB, seq int) error,
) (*schema.EventPayloadDurable, error) {
	payload, err := p.events.Publish(def, data, &core.EventPublishOptions{
		Commit: func(tx core.EventDB, seq int) error { return commit(tx, seq) },
	})
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}
