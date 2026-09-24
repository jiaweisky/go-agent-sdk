package route

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/llm/schema"
)

// Model is a model identity/defaults plus the configured route it was resolved
// from. The route-holding concrete type lives here because the route package
// imports schema; schema.ModelSchema stays the consumer-facing interface.
type Model struct {
	schema.Model
	Route Route
}

// GetRouteDefaults returns the route's defaults, or nil when it carries none.
func (m Model) GetRouteDefaults() *schema.ModelDefaults {
	defaults := m.Route.Defaults
	if defaults.Limits == nil && defaults.Generation == nil && defaults.ProviderOptions == nil && defaults.Http == nil {
		return nil
	}
	return &schema.ModelDefaults{
		Limits:          defaults.Limits,
		Generation:      defaults.Generation,
		ProviderOptions: defaults.ProviderOptions,
		Http:            defaults.Http,
	}
}

// MarshalJSON serializes the embedded model only: the route carries func fields,
// which encoding/json cannot encode.
func (m Model) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Model)
}
