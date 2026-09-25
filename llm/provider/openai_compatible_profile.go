package provider

// OpenAICompatibleProfile is a known OpenAI-compatible provider and its base URL.
type OpenAICompatibleProfile struct {
	Provider string
	BaseURL  string
}

// OpenAICompatibleProfiles are the known OpenAI-compatible providers, keyed by provider id.
var OpenAICompatibleProfiles = map[string]OpenAICompatibleProfile{
	"baseten":    {Provider: "baseten", BaseURL: "https://inference.baseten.co/v1"},
	"cerebras":   {Provider: "cerebras", BaseURL: "https://api.cerebras.ai/v1"},
	"deepinfra":  {Provider: "deepinfra", BaseURL: "https://api.deepinfra.com/v1/openai"},
	"deepseek":   {Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1"},
	"fireworks":  {Provider: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1"},
	"groq":       {Provider: "groq", BaseURL: "https://api.groq.com/openai/v1"},
	"openrouter": {Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"},
	"togetherai": {Provider: "togetherai", BaseURL: "https://api.together.xyz/v1"},
	"xai":        {Provider: "xai", BaseURL: "https://api.x.ai/v1"},
}
