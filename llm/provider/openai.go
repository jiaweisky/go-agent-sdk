package provider

import (
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// OpenAIProviderID is the provider id models.dev uses for OpenAI.
const OpenAIProviderID = schema.ProviderID("openai")

// OpenAIConfig configures the OpenAI facade.
type OpenAIConfig struct {
	CommonConfig
	QueryParams map[string]string
}

// OpenAIProvider is a configured OpenAI facade: its id, its routes, and its model
// factory.
type OpenAIProvider struct {
	ID        schema.ProviderID
	Routes    []route.Route
	Model     func(id string, opts ...llm.ModelOptions) route.Model
	Responses func(id string, opts ...llm.ModelOptions) route.Model
	Chat      func(id string, opts ...llm.ModelOptions) route.Model
}

func openAIAuth(input OpenAIConfig) *route.Auth {
	return route.BearerAuth(route.BearerAuthOptions{Auth: input.Auth, APIKey: input.APIKey}, []string{"OPENAI_API_KEY"})
}

func configureOpenAIRoute(r route.Route, input OpenAIConfig) route.Route {
	var baseURL *string
	if input.BaseURL != "" {
		baseURL = &input.BaseURL
	}
	var query map[string]string
	if len(input.QueryParams) > 0 {
		query = input.QueryParams
	}
	return r.With(route.RoutePatch{
		Auth:     openAIAuth(input),
		Endpoint: &route.EndpointPatch{BaseURL: baseURL, Query: query},
	})
}

func openAIModelFactory(r route.Route, modelID string, baseOpts route.RouteDefaultsInput, textVerbosity *bool, modelOpts ...llm.ModelOptions) route.Model {
	merged := baseOpts
	merged.ProviderOptions = withOpenAIOptions(modelID, baseOpts.ProviderOptions, textVerbosity)
	return bindModel(r.With(route.RoutePatch{Defaults: &merged}), route.RouteModelInput{ID: modelID}, modelOpts...)
}

// NewOpenAI builds the OpenAI facade over the Responses and Chat routes.
func NewOpenAI(responsesRoute, chatRoute route.Route, input OpenAIConfig) OpenAIProvider {
	responsesRoute = configureOpenAIRoute(responsesRoute, input)
	chatRoute = configureOpenAIRoute(chatRoute, input)
	base := input.defaults()

	return OpenAIProvider{
		ID:     OpenAIProviderID,
		Routes: []route.Route{responsesRoute, chatRoute},
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			return openAIModelFactory(responsesRoute, id, base, ptr(true), opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) route.Model {
			return openAIModelFactory(responsesRoute, id, base, ptr(true), opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) route.Model {
			return openAIModelFactory(chatRoute, id, base, nil, opts...)
		},
	}
}
