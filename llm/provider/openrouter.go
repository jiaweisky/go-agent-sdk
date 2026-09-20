package provider

import (
	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/route/transport"
	"github.com/user/go-agent-sdk/llm/schema"
)

const OpenRouterProviderID = schema.ProviderID("openrouter")

type OpenRouterConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type OpenRouterProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) route.Model
}

func openRouterDefaultBaseURL() string {
	return Profiles["openrouter"].BaseURL
}

func openRouterBodyOptions(providerOptions schema.ProviderOptions) map[string]any {
	openrouter := providerOptions["openrouter"]
	if len(openrouter) == 0 {
		return nil
	}
	result := make(map[string]any)
	if usage, exists := openrouter["usage"]; exists {
		switch v := usage.(type) {
		case bool:
			if v {
				result["usage"] = map[string]any{"include": true}
			}
		case map[string]any:
			result["usage"] = v
		}
	}
	if reasoning, exists := openrouter["reasoning"]; exists {
		if m, ok := reasoning.(map[string]any); ok {
			result["reasoning"] = m
		}
	}
	if cacheKey, exists := openrouter["promptCacheKey"]; exists {
		if s, ok := cacheKey.(string); ok {
			result["prompt_cache_key"] = s
		}
	}
	return result
}

func openRouterProtocol(openAIChatProtocol route.Protocol) route.Protocol {
	baseFrom := openAIChatProtocol.Body.From
	return route.Protocol{
		ID: "openrouter-chat",
		Body: route.ProtocolBody{
			Schema: openAIChatProtocol.Body.Schema,
			From: func(request *schema.LLMRequest) (any, error) {
				body, err := baseFrom(request)
				if err != nil {
					return nil, err
				}
				bodyMap, ok := body.(map[string]any)
				if !ok {
					return body, nil
				}
				opts := openRouterBodyOptions(request.ProviderOptions)
				for k, v := range opts {
					bodyMap[k] = v
				}
				return bodyMap, nil
			},
		},
		Stream: openAIChatProtocol.Stream,
	}
}

func openRouterAuth(input OpenRouterConfig) *route.Auth {
	return route.BearerAuth(struct {
		Auth   *route.Auth
		APIKey *string
	}{Auth: input.Auth, APIKey: input.APIKey}, []string{"OPENROUTER_API_KEY"})
}

func NewOpenRouter(openAIChatProtocol route.Protocol, input OpenRouterConfig) OpenRouterProvider {
	baseURL := input.BaseURL
	if baseURL == "" {
		baseURL = openRouterDefaultBaseURL()
	}
	proto := openRouterProtocol(openAIChatProtocol)
	r := route.MakeFromTransport(route.MakeTransportInput{
		ID:        "openrouter",
		Provider:  string(OpenRouterProviderID),
		Protocol:  proto,
		Endpoint:  route.NewStaticPath("/chat/completions", route.WithBaseURL(baseURL)),
		Auth:      openRouterAuth(input),
		Transport: transport.SSEJson(),
	})
	defaults := route.RouteDefaultsInput{
		Headers:         input.Headers,
		Limits:          input.Limits,
		Generation:      input.Generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
	}
	r = r.With(route.RoutePatch{Defaults: &defaults})
	return OpenRouterProvider{
		ID: OpenRouterProviderID,
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
