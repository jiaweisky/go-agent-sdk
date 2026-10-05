package provider

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// GitHubCopilotProviderID is the provider id models.dev uses for GitHub Copilot.
const GitHubCopilotProviderID = schema.ProviderID("github-copilot")

var gitHubCopilotGPTModelRegex = regexp.MustCompile(`^gpt-(\d+)`)

// GitHubCopilotConfig configures the GitHub Copilot facade.
type GitHubCopilotConfig struct {
	CommonConfig
	Endpoint *string
}

// GitHubCopilotProvider is a configured GitHub Copilot facade: its id, its routes,
// and its model factory.
type GitHubCopilotProvider struct {
	ID        schema.ProviderID
	Routes    []route.Route
	Model     func(id string, opts ...llm.ModelOptions) (route.Model, error)
	Responses func(id string, opts ...llm.ModelOptions) (route.Model, error)
	Chat      func(id string, opts ...llm.ModelOptions) (route.Model, error)
}

// shouldUseResponsesAPI reports whether a Copilot model should use the Responses
// API rather than Chat Completions.
func shouldUseResponsesAPI(modelID string, endpoint *string) bool {
	if endpoint != nil {
		return *endpoint == "responses"
	}
	match := gitHubCopilotGPTModelRegex.FindStringSubmatch(modelID)
	if match == nil {
		return false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return false
	}
	return n >= 5 && !strings.HasPrefix(modelID, "gpt-5-mini")
}

func configureGitHubCopilotRoute(r route.Route, options GitHubCopilotConfig) route.Route {
	var baseURL *string
	if options.BaseURL != "" {
		baseURL = &options.BaseURL
	}
	return r.With(route.RoutePatch{
		Endpoint: &route.EndpointPatch{BaseURL: baseURL},
		Auth:     route.BearerAuth(route.BearerAuthOptions{Auth: options.Auth, APIKey: options.APIKey}, nil),
	})
}

// NewGitHubCopilot builds the Copilot facade over the Responses and Chat routes.
func NewGitHubCopilot(responsesRoute, chatRoute route.Route, options GitHubCopilotConfig) GitHubCopilotProvider {
	chatWithProvider := chatRoute.With(route.RoutePatch{Provider: string(GitHubCopilotProviderID)})
	responsesWithProvider := responsesRoute.With(route.RoutePatch{Provider: string(GitHubCopilotProviderID)})
	responsesCfg := configureGitHubCopilotRoute(responsesWithProvider, options)
	chatCfg := configureGitHubCopilotRoute(chatWithProvider, options)
	base := options.defaults()

	return GitHubCopilotProvider{
		ID:     GitHubCopilotProviderID,
		Routes: []route.Route{responsesCfg, chatCfg},
		Model: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			if shouldUseResponsesAPI(id, options.Endpoint) {
				return openAIModelFactory(responsesCfg, id, base, nil, opts...)
			}
			return openAIModelFactory(chatCfg, id, base, nil, opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return openAIModelFactory(responsesCfg, id, base, nil, opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return openAIModelFactory(chatCfg, id, base, nil, opts...)
		},
	}
}
