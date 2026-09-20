package core

import (
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/schema"
)

// NewSessionTodo wires the core event service into session.Todo.
func NewSessionTodo(db *database.Database, events Event) *session.Todo {
	return session.NewTodo(db, sessionEventPublisher{events: events})
}

type sessionEventPublisher struct{ events Event }

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
	payload, err := p.events.Publish(def, data, &EventPublishOptions{
		Commit: func(tx EventDB, seq int) error { return commit(tx, seq) },
	})
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}
