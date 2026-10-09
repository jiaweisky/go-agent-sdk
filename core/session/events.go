package session

import "github.com/jiaweisky/go-agent-sdk/pkg/schema"

// EventPublisher is the narrow event surface the session package needs. It
// returns the durable pointer (nil for live-only events) so callers can read
// the aggregate sequence.
type EventPublisher interface {
	Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error)
}
