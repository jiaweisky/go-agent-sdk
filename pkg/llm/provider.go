package llm

import "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"

// ModelOptions overrides a model's defaults and compatibility rules.
type ModelOptions struct {
	Defaults      *schema.ModelDefaults
	Compatibility *schema.ModelCompatibility
}
