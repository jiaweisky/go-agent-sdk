package provider

import (
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const XAIProviderID = schema.ProviderID("xai")

type XAIConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type XAIProvider struct {
	ID        schema.ProviderID
	Routes    []route.Route
	Model     func(id string, opts ...llm.ModelOptions) route.Model
	Responses func(id string, opts ...llm.ModelOptions) route.Model
	Chat      func(id string, opts ...llm.ModelOptions) route.Model
}

func xaiDefaultBaseURL() string {
	return Profiles["xai"].BaseURL
}

func xaiAuth(input XAIConfig) *route.Auth {
	return route.BearerAuth(struct {
		Auth   *route.Auth
		APIKey *string
	}{Auth: input.Auth, APIKey: input.APIKey}, []string{"XAI_API_KEY"})
}

func xaiDefaults(input XAIConfig) route.RouteDefaultsInput {
	return route.RouteDefaultsInput{
		Headers:         input.Headers,
		Limits:          input.Limits,
		Generation:      input.Generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
	}
}

func configureXAIRoute(r route.Route, input XAIConfig, baseURL string) route.Route {
	defaults := xaiDefaults(input)
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

func NewXAI(responsesRoute, chatRoute route.Route, input XAIConfig) XAIProvider {
	baseURL := xaiDefaultBaseURL()
	respCfg := configureXAIRoute(responsesRoute, input, baseURL)
	chatCfg := configureXAIRoute(chatRoute, input, baseURL)
	return XAIProvider{
		ID:     XAIProviderID,
		Routes: []route.Route{respCfg, chatCfg},
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			return modelFactory(respCfg, id, xaiDefaults(input), nil, opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) route.Model {
			return modelFactory(respCfg, id, xaiDefaults(input), nil, opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) route.Model {
			return modelFactory(chatCfg, id, xaiDefaults(input), nil, opts...)
		},
	}
}
