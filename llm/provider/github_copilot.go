package provider

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// GitHubCopilotProviderID is the provider id models.dev uses for GitHub Copilot.
const GitHubCopilotProviderID = schema.ProviderID("github-copilot")

var gptModelRegex = regexp.MustCompile(`^gpt-(\d+)`)

// GitHubCopilotConfig configures the GitHub Copilot facade.
type GitHubCopilotConfig struct {
	Auth            *route.Auth
	APIKey          *string
	BaseURL         string
	Endpoint        *string
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

// GitHubCopilotProvider is a configured GitHub Copilot facade: its id, its routes,
// and its model factory.
type GitHubCopilotProvider struct {
	ID        schema.ProviderID
	Routes    []route.Route
	Model     func(id string, opts ...llm.ModelOptions) route.Model
	Responses func(id string, opts ...llm.ModelOptions) route.Model
	Chat      func(id string, opts ...llm.ModelOptions) route.Model
}

// ShouldUseResponsesAPI reports whether a Copilot model should use the Responses
// API rather than Chat Completions.
func ShouldUseResponsesAPI(modelID string, endpoint *string) bool {
	if endpoint != nil {
		return *endpoint == "responses"
	}
	match := gptModelRegex.FindStringSubmatch(modelID)
	if match == nil {
		return false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return false
	}
	return n >= 5 && !strings.HasPrefix(modelID, "gpt-5-mini")
}

func ghCopilotDefaults(options GitHubCopilotConfig) route.RouteDefaultsInput {
	return route.RouteDefaultsInput{
		Headers:         options.Headers,
		Limits:          options.Limits,
		Generation:      options.Generation,
		ProviderOptions: options.ProviderOptions,
		Http:            options.Http,
	}
}

func configGhCopilotRoute(r route.Route, options GitHubCopilotConfig) route.Route {
	var baseURL *string
	if options.BaseURL != "" {
		baseURL = &options.BaseURL
	}
	return r.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth: route.BearerAuth(struct {
			Auth   *route.Auth
			APIKey *string
		}{Auth: options.Auth, APIKey: options.APIKey}, nil),
	})
}

// NewGitHubCopilot builds the Copilot facade over the Responses and Chat routes.
func NewGitHubCopilot(responsesRoute, chatRoute route.Route, options GitHubCopilotConfig) GitHubCopilotProvider {
	chatWithProvider := chatRoute.With(route.RoutePatch{Provider: string(GitHubCopilotProviderID)})
	responsesWithProvider := responsesRoute.With(route.RoutePatch{Provider: string(GitHubCopilotProviderID)})
	responsesCfg := configGhCopilotRoute(responsesWithProvider, options)
	chatCfg := configGhCopilotRoute(chatWithProvider, options)
	base := ghCopilotDefaults(options)

	return GitHubCopilotProvider{
		ID:     GitHubCopilotProviderID,
		Routes: []route.Route{responsesCfg, chatCfg},
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			if ShouldUseResponsesAPI(id, options.Endpoint) {
				return modelFactory(responsesCfg, id, base, nil, opts...)
			}
			return modelFactory(chatCfg, id, base, nil, opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) route.Model {
			return modelFactory(responsesCfg, id, base, nil, opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) route.Model {
			return modelFactory(chatCfg, id, base, nil, opts...)
		},
	}
}
