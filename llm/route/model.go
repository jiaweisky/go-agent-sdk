package route

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/llm/schema"
)

// Model mirrors the LLM Model: model identity/defaults plus the configured route
// it was resolved from. TS keeps `route` on Model directly; Go splits the
// schema/route packages (route imports schema), so the route-holding concrete
// Model lives here while schema.ModelSchema stays the consumer interface.
type Model struct {
	schema.Model
	Route Route
}

// GetRouteDefaults mirrors `model.route.defaults` for consumers that only have
// the schema.ModelSchema interface.
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

// MarshalJSON serializes the embedded model only. The route carries func fields
// that json.Marshal rejects; TS's JSON.stringify silently drops them.
func (m Model) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Model)
}
