package llm

import "github.com/user/go-agent-sdk/llm/schema"

// ModelOptions overrides a model's defaults and compatibility rules.
type ModelOptions struct {
	Defaults      *schema.ModelDefaults
	Compatibility *schema.ModelCompatibility
}

// ModelFactory builds a model for an id, applying the given overrides.
type ModelFactory func(id string, opts ...ModelOptions) Model

// Definition describes a provider: its id, its default model factory, and the
// factories for its named APIs.
type Definition struct {
	ID    ProviderID
	Model ModelFactory
	Apis  map[string]ModelFactory
}

// NewDefinition returns def unchanged; it marks a provider definition at the
// call site.
func NewDefinition(def Definition) Definition {
	return def
}
