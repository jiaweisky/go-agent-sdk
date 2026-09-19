package provider

import (
	"fmt"

	"github.com/user/go-agent-sdk/llm"
	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

const AmazonAmazonBedrockProviderID = schema.ProviderID("amazon-bedrock")

type AmazonBedrockConfig struct {
	APIKey          *string
	Region          string
	BaseURL         string
	Credentials     *utils.Credentials
	Headers         map[string]string
	Limits          *schema.ModelLimits
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
}

type AmazonBedrockProvider struct {
	ID    schema.ProviderID
	Model func(id string, opts ...llm.ModelOptions) schema.Model
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
	return route.RouteDefaultsInput{
		Headers:         input.Headers,
		Limits:          input.Limits,
		Generation:      input.Generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
	}
}

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
		Provider: string(AmazonAmazonBedrockProviderID),
		Endpoint: &route.EndpointPatch{BaseURL: &baseURL},
		Auth:     bedrockAuth(input),
		Defaults: &defaults,
	})
	return AmazonBedrockProvider{
		ID: AmazonAmazonBedrockProviderID,
		Model: func(id string, opts ...llm.ModelOptions) schema.Model {
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
