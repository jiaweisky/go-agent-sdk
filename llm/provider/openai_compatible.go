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
	// Configure mirrors the facade's `configure`: it rebuilds the facade with
	// new options. For profile facades the provider id is fixed to the profile
	// and BaseURL falls back to the profile default.
	Configure func(OpenAICompatibleGenericOptions) OpenAICompatibleProvider
}

// OpenAICompatibleRoutes mirrors the module-level `routes` export.
var OpenAICompatibleRoutes []route.Route

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
		Configure: func(next OpenAICompatibleGenericOptions) OpenAICompatibleProvider {
			return configure(openAICompatibleChatRoute, next)
		},
	}
}

func define(openAICompatibleChatRoute route.Route, profile OpenAICompatibleProfile) OpenAICompatibleProvider {
	facade := configure(openAICompatibleChatRoute, OpenAICompatibleGenericOptions{
		Provider: profile.Provider,
		BaseURL:  profile.BaseURL,
	})
	// Profile facades keep the provider id fixed and fall back to the profile
	// BaseURL, mirroring `define`'s configureProfile.
	facade.Configure = func(next OpenAICompatibleGenericOptions) OpenAICompatibleProvider {
		next.Provider = profile.Provider
		if next.BaseURL == "" {
			next.BaseURL = profile.BaseURL
		}
		return configure(openAICompatibleChatRoute, next)
	}
	return facade
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
	OpenAICompatibleRoutes = []route.Route{openAICompatibleChatRoute}
	Baseten = define(openAICompatibleChatRoute, Profiles["baseten"])
	Cerebras = define(openAICompatibleChatRoute, Profiles["cerebras"])
	DeepInfra = define(openAICompatibleChatRoute, Profiles["deepinfra"])
	DeepSeek = define(openAICompatibleChatRoute, Profiles["deepseek"])
	Fireworks = define(openAICompatibleChatRoute, Profiles["fireworks"])
	Groq = define(openAICompatibleChatRoute, Profiles["groq"])
	TogetherAI = define(openAICompatibleChatRoute, Profiles["togetherai"])
}
