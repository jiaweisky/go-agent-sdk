package provider

import (
	"os"

	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const AnthropicProviderID = schema.ProviderID("anthropic")

type AnthropicConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type AnthropicProvider struct {
	ID     schema.ProviderID
	Routes []route.Route
	Model  func(id string, opts ...llm.ModelOptions) route.Model
}

func auth(input AnthropicConfig) *route.Auth {
	if input.Auth != nil {
		return input.Auth
	}
	cred := route.NewValue("")
	if input.APIKey != nil {
		cred = route.NewValue(*input.APIKey)
	}
	envVal := os.Getenv("ANTHROPIC_API_KEY")
	if envVal != "" {
		cred = cred.OrElse(route.NewValue(envVal))
	}
	return cred.Header("x-api-key")
}

func configuredRoute(anthropicMessagesRoute route.Route, input AnthropicConfig) route.Route {
	defaults := route.RouteDefaultsInput{
		Headers:         input.Headers,
		Limits:          input.Limits,
		Generation:      input.Generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
	}
	var baseURL *string
	if input.BaseURL != "" {
		baseURL = &input.BaseURL
	}
	return anthropicMessagesRoute.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth:     auth(input),
		Defaults: &defaults,
	})
}

func NewAnthropic(anthropicMessagesRoute route.Route, input AnthropicConfig) AnthropicProvider {
	r := configuredRoute(anthropicMessagesRoute, input)
	return AnthropicProvider{
		ID:     AnthropicProviderID,
		Routes: []route.Route{r},
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			modelInput := route.RouteModelInput{ID: id}
			if len(opts) > 0 {
				opt := opts[0]
				modelInput.Defaults = opt.Defaults
				modelInput.Compatibility = opt.Compatibility
			}
			return r.Model(modelInput)
		},
	}
}
