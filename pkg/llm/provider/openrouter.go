package provider

import (
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route/transport"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// OpenRouterProviderID is the provider id models.dev uses for OpenRouter.
const OpenRouterProviderID = schema.ProviderID("openrouter")

// OpenRouterConfig configures the OpenRouter facade.
type OpenRouterConfig struct {
	CommonConfig
}

// OpenRouterProvider is a configured OpenRouter facade: its id, its routes, and
// its model factory.
type OpenRouterProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) (route.Model, error)
}

func openRouterDefaultBaseURL() string {
	return OpenAICompatibleProfiles["openrouter"].BaseURL
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
	return route.BearerAuth(
		route.BearerAuthOptions{Auth: input.Auth, APIKey: input.APIKey},
		[]string{"OPENROUTER_API_KEY"},
	)
}

// NewOpenRouter builds the OpenRouter facade over the OpenAI Chat protocol.
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
	defaults := input.defaults()
	r = r.With(route.RoutePatch{Defaults: &defaults})
	return OpenRouterProvider{
		ID: OpenRouterProviderID,
		Model: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return bindModel(r, route.RouteModelInput{ID: id}, opts...)
		},
	}
}
