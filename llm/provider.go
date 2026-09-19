package llm

import "github.com/user/go-agent-sdk/llm/schema"

type ModelOptions struct {
	Defaults      *schema.ModelDefaults
	Compatibility *schema.ModelCompatibility
}

type ModelFactory func(id string, opts ...ModelOptions) Model

type Definition struct {
	ID    ProviderID
	Model ModelFactory
	Apis  map[string]ModelFactory
}

func NewDefinition(def Definition) Definition {
	return def
}
