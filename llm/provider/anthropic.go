package provider

import (
	"os"

	"github.com/jiaweisky/go-agent-sdk/llm"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// AnthropicProviderID is the provider id models.dev uses for Anthropic.
const AnthropicProviderID = schema.ProviderID("anthropic")

// AnthropicConfig configures the Anthropic facade.
type AnthropicConfig struct {
	CommonConfig
}

// AnthropicProvider is a configured Anthropic facade: its id, its route, and its
// model factory.
type AnthropicProvider struct {
	ID     schema.ProviderID
	Routes []route.Route
	Model  func(id string, opts ...llm.ModelOptions) (route.Model, error)
}

func anthropicAuth(input AnthropicConfig) *route.Auth {
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

func configureAnthropicRoute(anthropicMessagesRoute route.Route, input AnthropicConfig) route.Route {
	defaults := input.defaults()
	var baseURL *string
	if input.BaseURL != "" {
		baseURL = &input.BaseURL
	}
	return anthropicMessagesRoute.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth:     anthropicAuth(input),
		Defaults: &defaults,
	})
}

// NewAnthropic builds the Anthropic facade over the Anthropic Messages route.
func NewAnthropic(anthropicMessagesRoute route.Route, input AnthropicConfig) AnthropicProvider {
	r := configureAnthropicRoute(anthropicMessagesRoute, input)
	return AnthropicProvider{
		ID:     AnthropicProviderID,
		Routes: []route.Route{r},
		Model: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return bindModel(r, route.RouteModelInput{ID: id}, opts...)
		},
	}
}
