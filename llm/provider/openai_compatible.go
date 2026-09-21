package provider

import (
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const OpenAICompatibleProviderID = schema.ProviderID("openai-compatible")

type OpenAICompatibleGenericOptions struct {
	Auth            *route.Auth
	APIKey          *string
	Provider        string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type OpenAICompatibleFamilyOptions struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type OpenAICompatibleProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) route.Model
}

func configure(openAICompatibleChatRoute route.Route, input OpenAICompatibleGenericOptions) OpenAICompatibleProvider {
	prov := input.Provider
	if prov == "" {
		prov = "openai-compatible"
	}
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
	r := openAICompatibleChatRoute.With(route.RoutePatch{
		Provider: prov,
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth: route.BearerAuth(struct {
			Auth   *route.Auth
			APIKey *string
		}{Auth: input.Auth, APIKey: input.APIKey}, nil),
		Defaults: &defaults,
	})
	return OpenAICompatibleProvider{
		ID: schema.ProviderID(prov),
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			modelInput := route.RouteModelInput{
				ID:       id,
				Provider: prov,
			}
			if len(opts) > 0 {
				opt := opts[0]
				modelInput.Defaults = opt.Defaults
				modelInput.Compatibility = opt.Compatibility
			}
			return r.Model(modelInput)
		},
	}
}

func define(openAICompatibleChatRoute route.Route, profile OpenAICompatibleProfile) OpenAICompatibleProvider {
	return configure(openAICompatibleChatRoute, OpenAICompatibleGenericOptions{
		Provider: profile.Provider,
		BaseURL:  profile.BaseURL,
	})
}

// NewOpenAICompatible mirrors OpenAICompatible.configure: it builds a facade for
// a generic OpenAI Chat-compatible endpoint. When input.Provider is empty the
// provider id defaults to "openai-compatible".
func NewOpenAICompatible(openAICompatibleChatRoute route.Route, input OpenAICompatibleGenericOptions) OpenAICompatibleProvider {
	return configure(openAICompatibleChatRoute, input)
}

var Baseten OpenAICompatibleProvider
var Cerebras OpenAICompatibleProvider
var DeepInfra OpenAICompatibleProvider
var DeepSeek OpenAICompatibleProvider
var Fireworks OpenAICompatibleProvider
var Groq OpenAICompatibleProvider
var TogetherAI OpenAICompatibleProvider

func InitOpenAICompatible(openAICompatibleChatRoute route.Route) {
	Baseten = define(openAICompatibleChatRoute, Profiles["baseten"])
	Cerebras = define(openAICompatibleChatRoute, Profiles["cerebras"])
	DeepInfra = define(openAICompatibleChatRoute, Profiles["deepinfra"])
	DeepSeek = define(openAICompatibleChatRoute, Profiles["deepseek"])
	Fireworks = define(openAICompatibleChatRoute, Profiles["fireworks"])
	Groq = define(openAICompatibleChatRoute, Profiles["groq"])
	TogetherAI = define(openAICompatibleChatRoute, Profiles["togetherai"])
}
