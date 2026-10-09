package provider

import (
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// OpenAICompatibleProviderID is the provider id used when a generic
// OpenAI-compatible config names none.
const OpenAICompatibleProviderID = schema.ProviderID("openai-compatible")

// OpenAICompatibleGenericOptions configures a generic OpenAI-compatible facade.
type OpenAICompatibleGenericOptions struct {
	CommonConfig
	Provider string
}

// OpenAICompatibleProvider is a configured OpenAI-compatible facade.
type OpenAICompatibleProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) (route.Model, error)
	// Configure rebuilds the facade with
	// new options. For profile facades the provider id is fixed to the profile
	// and BaseURL falls back to the profile default.
	Configure func(OpenAICompatibleGenericOptions) OpenAICompatibleProvider
}

// OpenAICompatibleRoutes is the route every OpenAI-compatible facade is built on.
var OpenAICompatibleRoutes []route.Route

func configureOpenAICompatible(
	openAICompatibleChatRoute route.Route,
	input OpenAICompatibleGenericOptions,
) OpenAICompatibleProvider {
	prov := input.Provider
	if prov == "" {
		prov = string(OpenAICompatibleProviderID)
	}
	defaults := input.defaults()
	var baseURL *string
	if input.BaseURL != "" {
		baseURL = &input.BaseURL
	}
	r := openAICompatibleChatRoute.With(route.RoutePatch{
		Provider: prov,
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth:     route.BearerAuth(route.BearerAuthOptions{Auth: input.Auth, APIKey: input.APIKey}, nil),
		Defaults: &defaults,
	})
	return OpenAICompatibleProvider{
		ID: schema.ProviderID(prov),
		Model: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return bindModel(r, route.RouteModelInput{ID: id, Provider: prov}, opts...)
		},
		Configure: func(next OpenAICompatibleGenericOptions) OpenAICompatibleProvider {
			return configureOpenAICompatible(openAICompatibleChatRoute, next)
		},
	}
}

func defineOpenAICompatibleProfile(
	openAICompatibleChatRoute route.Route,
	profile OpenAICompatibleProfile,
) OpenAICompatibleProvider {
	facade := configureOpenAICompatible(openAICompatibleChatRoute, OpenAICompatibleGenericOptions{
		CommonConfig: CommonConfig{BaseURL: profile.BaseURL},
		Provider:     profile.Provider,
	})
	// Profile facades keep the provider id fixed and fall back to the profile
	// BaseURL.
	facade.Configure = func(next OpenAICompatibleGenericOptions) OpenAICompatibleProvider {
		next.Provider = profile.Provider
		if next.BaseURL == "" {
			next.BaseURL = profile.BaseURL
		}
		return configureOpenAICompatible(openAICompatibleChatRoute, next)
	}
	return facade
}

// NewOpenAICompatible builds a facade for a generic OpenAI Chat-compatible
// endpoint. When input.Provider is empty the provider id defaults to
// "openai-compatible".
func NewOpenAICompatible(
	openAICompatibleChatRoute route.Route,
	input OpenAICompatibleGenericOptions,
) OpenAICompatibleProvider {
	return configureOpenAICompatible(openAICompatibleChatRoute, input)
}

// The known OpenAI-compatible families, built by InitOpenAICompatible.
var Baseten OpenAICompatibleProvider

var (
	Cerebras   OpenAICompatibleProvider
	DeepInfra  OpenAICompatibleProvider
	DeepSeek   OpenAICompatibleProvider
	Fireworks  OpenAICompatibleProvider
	Groq       OpenAICompatibleProvider
	TogetherAI OpenAICompatibleProvider
)

// InitOpenAICompatible builds the known OpenAI-compatible families over the given
// route.
func InitOpenAICompatible(openAICompatibleChatRoute route.Route) {
	OpenAICompatibleRoutes = []route.Route{openAICompatibleChatRoute}
	Baseten = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["baseten"])
	Cerebras = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["cerebras"])
	DeepInfra = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["deepinfra"])
	DeepSeek = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["deepseek"])
	Fireworks = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["fireworks"])
	Groq = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["groq"])
	TogetherAI = defineOpenAICompatibleProfile(openAICompatibleChatRoute, OpenAICompatibleProfiles["togetherai"])
}
