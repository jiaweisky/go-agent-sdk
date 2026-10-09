package provider

import (
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// XAIProviderID is the provider id models.dev uses for xAI.
const XAIProviderID = schema.ProviderID("xai")

// XAIConfig configures the xAI facade.
type XAIConfig struct {
	CommonConfig
}

// XAIProvider is a configured xAI facade: its id, its routes, and its model
// factory.
type XAIProvider struct {
	ID        schema.ProviderID
	Routes    []route.Route
	Model     func(id string, opts ...llm.ModelOptions) (route.Model, error)
	Responses func(id string, opts ...llm.ModelOptions) (route.Model, error)
	Chat      func(id string, opts ...llm.ModelOptions) (route.Model, error)
}

func xaiDefaultBaseURL() string {
	return OpenAICompatibleProfiles["xai"].BaseURL
}

func xaiAuth(input XAIConfig) *route.Auth {
	return route.BearerAuth(route.BearerAuthOptions{Auth: input.Auth, APIKey: input.APIKey}, []string{"XAI_API_KEY"})
}

func configureXAIRoute(r route.Route, input XAIConfig, baseURL string) route.Route {
	defaults := input.defaults()
	var epBaseURL *string
	if input.BaseURL != "" {
		epBaseURL = &input.BaseURL
	} else {
		epBaseURL = &baseURL
	}
	return r.With(route.RoutePatch{
		Provider: string(XAIProviderID),
		Endpoint: &route.EndpointPatch{BaseURL: epBaseURL},
		Auth:     xaiAuth(input),
		Defaults: &defaults,
	})
}

// NewXAI builds the xAI facade over the Responses and Chat routes.
func NewXAI(responsesRoute, chatRoute route.Route, input XAIConfig) XAIProvider {
	baseURL := xaiDefaultBaseURL()
	respCfg := configureXAIRoute(responsesRoute, input, baseURL)
	chatCfg := configureXAIRoute(chatRoute, input, baseURL)
	return XAIProvider{
		ID:     XAIProviderID,
		Routes: []route.Route{respCfg, chatCfg},
		Model: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return openAIModelFactory(respCfg, id, input.defaults(), nil, opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return openAIModelFactory(respCfg, id, input.defaults(), nil, opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return openAIModelFactory(chatCfg, id, input.defaults(), nil, opts...)
		},
	}
}
