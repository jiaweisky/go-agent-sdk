package provider

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/internal/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	coreschema "github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// AmazonBedrockProviderID is the provider id used for Bedrock.
const AmazonBedrockProviderID = schema.ProviderID("amazon-bedrock")

// AmazonBedrockConfig configures the Amazon Bedrock facade.
type AmazonBedrockConfig struct {
	CommonConfig
	Region      string
	Credentials *coreschema.AWSCredentials
}

// AmazonBedrockProvider is a configured Bedrock facade: its id, its route, and
// its model factory.
type AmazonBedrockProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) (route.Model, error)
}

func bedrockBaseURL(region string) string {
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
}

func bedrockAuth(input AmazonBedrockConfig) *route.Auth {
	if input.APIKey != nil {
		return route.Bearer(*input.APIKey)
	}
	signer := utils.SigV4(input.Credentials)
	return route.Custom(func(ai route.AuthInput) (map[string]string, error) {
		return signer(utils.AuthInput{
			URL:     ai.URL,
			Body:    ai.Body,
			Headers: ai.Headers,
		})
	})
}

func bedrockDefaults(input AmazonBedrockConfig) route.RouteDefaultsInput {
	return input.defaults()
}

// NewAmazonBedrock builds the Bedrock facade over the Bedrock Converse route.
func NewAmazonBedrock(bedrockConverseRoute route.Route, input AmazonBedrockConfig) AmazonBedrockProvider {
	region := input.Region
	if region == "" && input.Credentials != nil {
		region = input.Credentials.Region
	}
	if region == "" {
		region = "us-east-1"
	}
	baseURL := input.BaseURL
	if baseURL == "" {
		baseURL = bedrockBaseURL(region)
	}
	defaults := bedrockDefaults(input)
	r := bedrockConverseRoute.With(route.RoutePatch{
		Provider: string(AmazonBedrockProviderID),
		Endpoint: &route.EndpointPatch{BaseURL: &baseURL},
		Auth:     bedrockAuth(input),
		Defaults: &defaults,
	})
	return AmazonBedrockProvider{
		ID: AmazonBedrockProviderID,
		Model: func(id string, opts ...llm.ModelOptions) (route.Model, error) {
			return bindModel(r, route.RouteModelInput{ID: id}, opts...)
		},
	}
}
