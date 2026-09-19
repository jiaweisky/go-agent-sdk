package provider

type OpenAICompatibleProfile struct {
	Provider string
	BaseURL  string
}

var Profiles = map[string]OpenAICompatibleProfile{
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

var ByProvider map[string]OpenAICompatibleProfile

func init() {
	ByProvider = make(map[string]OpenAICompatibleProfile, len(Profiles))
	for _, p := range Profiles {
		ByProvider[p.Provider] = p
	}
}
