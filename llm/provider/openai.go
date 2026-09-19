package provider

import (
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const OpenAIProviderID = schema.ProviderID("openai")

type OpenAIConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	QueryParams     map[string]string
	ProviderOptions schema.ProviderOptions
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	Http            *schema.HttpOptions
}

type OpenAIProvider struct {
	ID                 schema.ProviderID
	Routes             []route.Route
	Model              func(id string, opts ...llm.ModelOptions) schema.Model
	Responses          func(id string, opts ...llm.ModelOptions) schema.Model
	ResponsesWebSocket func(id string, opts ...llm.ModelOptions) schema.Model
	Chat               func(id string, opts ...llm.ModelOptions) schema.Model
}

func openAIAuth(input OpenAIConfig) *route.Auth {
	return route.BearerAuth(struct {
		Auth   *route.Auth
		APIKey *string
	}{Auth: input.Auth, APIKey: input.APIKey}, []string{"OPENAI_API_KEY"})
}

func openAIDefaults(input OpenAIConfig) route.RouteDefaultsInput {
	return route.RouteDefaultsInput{
		Headers:         input.Headers,
		Limits:          input.Limits,
		Generation:      input.Generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
	}
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

func modelFactory(r route.Route, modelID string, baseOpts route.RouteDefaultsInput, textVerbosity *bool, modelOpts ...llm.ModelOptions) schema.Model {
	merged := baseOpts
	merged.ProviderOptions = withOpenAIOptions(modelID, baseOpts.ProviderOptions, textVerbosity)
	modelInput := route.RouteModelInput{ID: modelID}
	if len(modelOpts) > 0 {
		opt := modelOpts[0]
		modelInput.Defaults = opt.Defaults
		modelInput.Compatibility = opt.Compatibility
	}
	return r.With(route.RoutePatch{Defaults: &merged}).Model(modelInput)
}

func NewOpenAI(responsesRoute, responsesWebSocketRoute, chatRoute route.Route, input OpenAIConfig) OpenAIProvider {
	responsesRoute = configureOpenAIRoute(responsesRoute, input)
	responsesWebSocketRoute = configureOpenAIRoute(responsesWebSocketRoute, input)
	chatRoute = configureOpenAIRoute(chatRoute, input)
	base := openAIDefaults(input)

	return OpenAIProvider{
		ID:     OpenAIProviderID,
		Routes: []route.Route{responsesRoute, responsesWebSocketRoute, chatRoute},
		Model: func(id string, opts ...llm.ModelOptions) schema.Model {
			return modelFactory(responsesRoute, id, base, ptr(true), opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) schema.Model {
			return modelFactory(responsesRoute, id, base, ptr(true), opts...)
		},
		ResponsesWebSocket: func(id string, opts ...llm.ModelOptions) schema.Model {
			return modelFactory(responsesWebSocketRoute, id, base, ptr(true), opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) schema.Model {
			return modelFactory(chatRoute, id, base, nil, opts...)
		},
	}
}
