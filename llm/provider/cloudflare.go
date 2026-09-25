package provider

import (
	"fmt"
	"net/url"
	"os"

	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const (
	CloudflareAIGatewayProviderID = schema.ProviderID("cloudflare-ai-gateway")
	CloudflareWorkersAIProviderID = schema.ProviderID("cloudflare-workers-ai")
)

// CloudflareAIGatewayConfig configures the Cloudflare AI Gateway facade.
type CloudflareAIGatewayConfig struct {
	CommonConfig
	AccountID     string
	GatewayID     string
	GatewayAPIKey *string
}

// CloudflareWorkersAIConfig configures the Cloudflare Workers AI facade.
type CloudflareWorkersAIConfig struct {
	CommonConfig
	AccountID string
}

// CloudflareAIGatewayProvider is a configured AI Gateway facade.
type CloudflareAIGatewayProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) route.Model
}

// CloudflareWorkersAIProvider is a configured Workers AI facade.
type CloudflareWorkersAIProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) route.Model
}

// AIGatewayBaseURL renders the AI Gateway base URL for a config.
func AIGatewayBaseURL(input CloudflareAIGatewayConfig) string {
	if input.BaseURL != "" {
		return input.BaseURL
	}
	if input.AccountID == "" {
		panic("CloudflareAIGateway requires accountId unless baseURL is supplied")
	}
	gwID := input.GatewayID
	if gwID == "" {
		gwID = "default"
	}
	return fmt.Sprintf("https://gateway.ai.cloudflare.com/v1/%s/%s/compat",
		url.PathEscape(input.AccountID), url.PathEscape(gwID))
}

// WorkersAIBaseURL renders the Workers AI base URL for a config.
func WorkersAIBaseURL(input CloudflareWorkersAIConfig) string {
	if input.BaseURL != "" {
		return input.BaseURL
	}
	if input.AccountID == "" {
		panic("CloudflareWorkersAI requires accountId unless baseURL is supplied")
	}
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/v1",
		url.PathEscape(input.AccountID))
}

func cloudflareAIGatewayAuth(input CloudflareAIGatewayConfig) *route.Auth {
	if input.Auth != nil {
		return input.Auth
	}

	gatewayCred := route.NewValue("")
	if input.GatewayAPIKey != nil {
		gatewayCred = route.NewValue(*input.GatewayAPIKey)
	}
	for _, name := range []string{"CLOUDFLARE_API_TOKEN", "CF_AIG_TOKEN"} {
		envVal := os.Getenv(name)
		if envVal != "" {
			gatewayCred = gatewayCred.OrElse(route.NewValue(envVal))
		}
	}
	gatewayAuth := route.BearerHeader("cf-aig-authorization", gatewayCred)

	if input.APIKey == nil {
		return gatewayAuth
	}
	if input.GatewayAPIKey == nil {
		return route.Bearer(*input.APIKey)
	}
	return route.BearerHeader("cf-aig-authorization", *input.GatewayAPIKey).AndThen(route.Bearer(*input.APIKey))
}

func cloudflareWorkersAIAuth(input CloudflareWorkersAIConfig) *route.Auth {
	return route.BearerAuth(route.BearerAuthOptions{Auth: input.Auth, APIKey: input.APIKey},
		[]string{"CLOUDFLARE_API_KEY", "CLOUDFLARE_WORKERS_AI_TOKEN"})
}

// NewCloudflareAIGateway builds the AI Gateway facade over the OpenAI-compatible
// Chat route.
func NewCloudflareAIGateway(openAICompatibleChatRoute route.Route, input CloudflareAIGatewayConfig) CloudflareAIGatewayProvider {
	preconfigured := openAICompatibleChatRoute.With(route.RoutePatch{
		ID:       string(CloudflareAIGatewayProviderID),
		Provider: string(CloudflareAIGatewayProviderID),
	})
	defaults := input.defaults()
	r := preconfigured.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: ptr(AIGatewayBaseURL(input))},
		Auth:     cloudflareAIGatewayAuth(input),
		Defaults: &defaults,
	})
	return CloudflareAIGatewayProvider{
		ID: CloudflareAIGatewayProviderID,
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			return bindModel(r, route.RouteModelInput{ID: id}, opts...)
		},
	}
}

// NewCloudflareWorkersAI builds the Workers AI facade over the OpenAI-compatible
// Chat route.
func NewCloudflareWorkersAI(openAICompatibleChatRoute route.Route, input CloudflareWorkersAIConfig) CloudflareWorkersAIProvider {
	preconfigured := openAICompatibleChatRoute.With(route.RoutePatch{
		ID:       string(CloudflareWorkersAIProviderID),
		Provider: string(CloudflareWorkersAIProviderID),
	})
	defaults := input.defaults()
	r := preconfigured.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: ptr(WorkersAIBaseURL(input))},
		Auth:     cloudflareWorkersAIAuth(input),
		Defaults: &defaults,
	})
	return CloudflareWorkersAIProvider{
		ID: CloudflareWorkersAIProviderID,
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			return bindModel(r, route.RouteModelInput{ID: id}, opts...)
		},
	}
}
