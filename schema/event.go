package schema

import "fmt"

type EventID string

func (id EventID) String() string {
	return string(id)
}

func NewEventID() EventID {
	return EventID("evt_" + Ascending())
}

type EventDurable struct {
	Version   int
	Aggregate string
}

type EventPayloadDurable struct {
	AggregateID string
	Seq         int
	Version     int
}

// EventDefinition describes one event type. Data is the field schema keyed by
// field name; id gives each definition a distinct identity for duplicate
// detection.
type EventDefinition struct {
	Type    string
	Durable *EventDurable
	Data    map[string]any

	id *int
}

// DefineInput is the raw input to Define.
type DefineInput struct {
	Type    string
	Durable *EventDurable
	Schema  map[string]any
}

// Define builds an event definition from its type, optional durability and
// field schema.
func Define(input DefineInput) EventDefinition {
	return EventDefinition{
		Type:    input.Type,
		Durable: input.Durable,
		Data:    input.Schema,
		id:      new(int),
	}
}

type EventPayload struct {
	ID       EventID
	Type     string
	Data     any
	Durable  *EventPayloadDurable
	Location *LocationRef
	Metadata map[string]any
}

// Inventory returns the definitions as a single list.
func Inventory(definitions ...EventDefinition) []EventDefinition {
	return definitions
}

// Latest maps each event type to its highest durable version. Two distinct
// definitions of the same version panic.
func Latest(definitions []EventDefinition) map[string]EventDefinition {
	result := make(map[string]EventDefinition)
	for _, definition := range definitions {
		existing, ok := result[definition.Type]
		if !ok {
			result[definition.Type] = definition
			continue
		}
		if definition.Durable != nil && existing.Durable != nil && definition.Durable.Version != existing.Durable.Version {
			if definition.Durable.Version > existing.Durable.Version {
				result[definition.Type] = definition
			}
			continue
		}
		if definition.id != existing.id {
			panic(fmt.Sprintf("Duplicate latest event definition for %s", definition.Type))
		}
	}
	return result
}

// VersionedType is the storage type of a durable event at a version.
func VersionedType(eventType string, version int) string {
	return fmt.Sprintf("%s.%d", eventType, version)
}

// Durable maps each durable event's versioned type to its definition. A
// duplicate versioned type panics.
func Durable(definitions []EventDefinition) map[string]EventDefinition {
	result := make(map[string]EventDefinition)
	for _, definition := range definitions {
		if definition.Durable == nil {
			continue
		}
		key := VersionedType(definition.Type, definition.Durable.Version)
		if _, exists := result[key]; exists {
			panic(fmt.Sprintf("Duplicate durable event definition for %s", key))
		}
		result[key] = definition
	}
	return result
}
