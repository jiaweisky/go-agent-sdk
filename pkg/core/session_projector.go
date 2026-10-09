package core

import (
	"github.com/jiaweisky/go-agent-sdk/internal/core/session"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// RegisterSessionProjectors wires the session event projectors into the event
// service. It bridges core.EventDB (the event transaction) to session.DB.
func RegisterSessionProjectors(events Event) error {
	return session.RegisterProjectors(sessionProjectRegistrar{events: events})
}

type sessionProjectRegistrar struct{ events Event }

func (r sessionProjectRegistrar) Project(
	def schema.EventDefinition,
	projector func(schema.EventPayload, session.DB) error,
) error {
	return r.events.Project(def, func(event schema.EventPayload, tx EventDB) error {
		return projector(event, tx)
	})
}
