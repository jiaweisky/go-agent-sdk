package provider

import (
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// CommonConfig is the configuration every provider facade accepts. Facade
// configs embed it and add their provider-specific fields.
type CommonConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

// defaults builds the route defaults this config contributes.
func (c CommonConfig) defaults() route.RouteDefaultsInput {
	return route.RouteDefaultsInput{
		Headers:         c.Headers,
		Limits:          c.Limits,
		Generation:      c.Generation,
		ProviderOptions: c.ProviderOptions,
		Http:            c.Http,
	}
}

// bindModel binds a model id to a route, applying the caller's overrides.
func bindModel(r route.Route, input route.RouteModelInput, opts ...llm.ModelOptions) (route.Model, error) {
	if len(opts) > 0 {
		input.Defaults = opts[0].Defaults
		input.Compatibility = opts[0].Compatibility
	}
	return r.Model(input)
}
