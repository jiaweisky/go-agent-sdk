package provider

import (
	"fmt"
	"os"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// AzureProviderID is the provider id models.dev uses for Azure OpenAI.
const AzureProviderID = schema.ProviderID("azure")

// AzureConfig configures the Azure OpenAI facade.
type AzureConfig struct {
	CommonConfig
	ResourceName      string
	APIVersion        string
	QueryParams       map[string]string
	UseCompletionURLs bool
}

// AzureProvider is a configured Azure OpenAI facade: its id, its routes, and its
// model factory.
type AzureProvider struct {
	ID        schema.ProviderID
	Routes    []route.Route
	Model     func(id string, opts ...llm.ModelOptions) route.Model
	Responses func(id string, opts ...llm.ModelOptions) route.Model
	Chat      func(id string, opts ...llm.ModelOptions) route.Model
}

func resourceBaseURL(resourceName string) string {
	return fmt.Sprintf("https://%s.openai.azure.com/openai/v1", strings.TrimSpace(resourceName))
}

func azureAuth(input AzureConfig) *route.Auth {
	if input.Auth != nil {
		return input.Auth
	}
	cred := route.NewValue("")
	if input.APIKey != nil {
		cred = route.NewValue(*input.APIKey)
	}
	envVal := os.Getenv("AZURE_OPENAI_API_KEY")
	if envVal != "" {
		cred = cred.OrElse(route.NewValue(envVal))
	}
	return route.RemoveHeader("authorization").AndThen(cred.Header("api-key"))
}

func azureEndpoint(input AzureConfig) *route.EndpointPatch {
	baseURL := input.BaseURL
	if baseURL == "" {
		baseURL = resourceBaseURL(input.ResourceName)
	}
	query := make(map[string]string)
	if input.APIVersion != "" {
		query["api-version"] = input.APIVersion
	} else {
		query["api-version"] = "v1"
	}
	for k, v := range input.QueryParams {
		query[k] = v
	}
	return &route.EndpointPatch{
		BaseURL: &baseURL,
		Query:   query,
	}
}

// NewAzure builds the Azure OpenAI facade over the Responses and Chat routes.
func NewAzure(responsesRoute, chatRoute route.Route, input AzureConfig) AzureProvider {
	responsesPre := responsesRoute.With(route.RoutePatch{
		ID:       "azure-openai-responses",
		Provider: string(AzureProviderID),
	})
	chatPre := chatRoute.With(route.RoutePatch{
		ID:       "azure-openai-chat",
		Provider: string(AzureProviderID),
	})
	a := azureAuth(input)
	ep := azureEndpoint(input)
	respCfg := responsesPre.With(route.RoutePatch{Auth: a, Endpoint: ep})
	chatCfg := chatPre.With(route.RoutePatch{Auth: a, Endpoint: ep})
	base := input.defaults()

	return AzureProvider{
		ID:     AzureProviderID,
		Routes: []route.Route{respCfg, chatCfg},
		Model: func(id string, opts ...llm.ModelOptions) route.Model {
			if input.UseCompletionURLs {
				return openAIModelFactory(chatCfg, id, base, nil, opts...)
			}
			return openAIModelFactory(respCfg, id, base, nil, opts...)
		},
		Responses: func(id string, opts ...llm.ModelOptions) route.Model {
			return openAIModelFactory(respCfg, id, base, nil, opts...)
		},
		Chat: func(id string, opts ...llm.ModelOptions) route.Model {
			return openAIModelFactory(chatCfg, id, base, nil, opts...)
		},
	}
}
