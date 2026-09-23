package agent

import (
	"fmt"

	"github.com/user/go-agent-sdk/schema"
)

// Provider API names accepted by NewModelInfo.
const (
	ModelAPIOpenAI           = "openai"
	ModelAPIAnthropic        = "anthropic"
	ModelAPIOpenAICompatible = "openai-compatible"
)

// NewModelInfo builds a catalog-style ModelInfo for one of the SDK's supported
// provider APIs ("openai", "anthropic", "openai-compatible"). baseURL is the
// provider endpoint; it is required for openai-compatible and optional
// otherwise. Hand the result to ClientOptions.ModelInfo.
func NewModelInfo(api, modelID, baseURL string, limit schema.ModelLimit) (schema.ModelInfo, error) {
	var pkg string
	switch api {
	case ModelAPIOpenAI:
		pkg = "@ai-sdk/openai"
	case ModelAPIAnthropic:
		pkg = "@ai-sdk/anthropic"
	case ModelAPIOpenAICompatible:
		pkg = "@ai-sdk/openai-compatible"
		if baseURL == "" {
			return schema.ModelInfo{}, fmt.Errorf("openai-compatible requires a base URL")
		}
	default:
		return schema.ModelInfo{}, fmt.Errorf("unsupported model api %q", api)
	}
	info := schema.ModelInfoEmpty(schema.ProviderID(api), schema.ModelID(modelID))
	info.API.Type = "aisdk"
	info.API.Package = pkg
	if baseURL != "" {
		value := baseURL
		info.API.URL = &value
	}
	info.Limit = limit
	return info, nil
}
