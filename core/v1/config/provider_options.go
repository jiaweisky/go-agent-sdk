package config

import "strings"

type optionsMap = map[string]any

// V1ProviderOptionsResult mirrors ConfigProviderOptionsV1.ProviderResult.
type V1ProviderOptionsResult struct {
	Headers  map[string]string
	Body     map[string]any
	URL      *string
	Settings map[string]any
}

// V1ProviderOptionsLowerer mirrors ConfigProviderOptionsV1.Lowerer.
type V1ProviderOptionsLowerer struct {
	Provider func(options optionsMap) V1ProviderOptionsResult
	Request  func(options optionsMap) map[string]any
}

var v1RawLowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		return V1ProviderOptionsResult{Body: v1Clone(options)}
	},
	Request: v1Clone,
}

var v1OpenAILowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		headers := map[string]*string{
			"Authorization":       v1Bearer(options["apiKey"]),
			"OpenAI-Organization": v1String(options["organization"]),
			"OpenAI-Project":      v1String(options["project"]),
		}
		for key, value := range v1HeaderMap(options["headers"]) {
			value := value
			headers[key] = &value
		}
		return V1ProviderOptionsResult{
			URL:      v1String(options["baseURL"]),
			Headers:  v1Compact(headers),
			Body:     v1Body(options["body"]),
			Settings: v1Omit(options, "apiKey", "baseURL", "organization", "project", "headers", "body"),
		}
	},
	Request: func(options optionsMap) map[string]any {
		result := v1Snake(options)
		if options["reasoningEffort"] != nil || options["reasoningSummary"] != nil {
			reasoning := v1Record(result["reasoning"])
			if options["reasoningEffort"] != nil {
				reasoning["effort"] = options["reasoningEffort"]
			}
			if options["reasoningSummary"] != nil {
				reasoning["summary"] = options["reasoningSummary"]
			}
			result["reasoning"] = reasoning
			delete(result, "reasoning_effort")
			delete(result, "reasoning_summary")
		}
		if options["textVerbosity"] != nil {
			text := v1Record(result["text"])
			text["verbosity"] = options["textVerbosity"]
			result["text"] = text
			delete(result, "text_verbosity")
		}
		return result
	},
}

var v1AnthropicLowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		headers := map[string]*string{
			"x-api-key": v1String(options["apiKey"]),
		}
		if options["authToken"] != nil {
			headers["Authorization"] = v1Bearer(options["authToken"])
		}
		for key, value := range v1HeaderMap(options["headers"]) {
			value := value
			headers[key] = &value
		}
		return V1ProviderOptionsResult{
			URL:      v1String(options["baseURL"]),
			Headers:  v1Compact(headers),
			Body:     v1Body(options["body"]),
			Settings: v1Omit(options, "apiKey", "authToken", "baseURL", "headers", "body"),
		}
	},
	Request: func(options optionsMap) map[string]any {
		result := v1Snake(options)
		if options["effort"] != nil || options["taskBudget"] != nil {
			result["output_config"] = v1CompactUnknown(map[string]any{
				"effort":      options["effort"],
				"task_budget": options["taskBudget"],
			})
			delete(result, "effort")
			delete(result, "task_budget")
		}
		if metadata, ok := options["metadata"].(optionsMap); ok && metadata["userId"] != nil {
			target := v1Record(result["metadata"])
			target["user_id"] = metadata["userId"]
			result["metadata"] = target
		}
		return result
	},
}

var v1GoogleLowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		headers := map[string]*string{
			"x-goog-api-key": v1String(options["apiKey"]),
		}
		for key, value := range v1HeaderMap(options["headers"]) {
			value := value
			headers[key] = &value
		}
		return V1ProviderOptionsResult{
			URL:      v1String(options["baseURL"]),
			Headers:  v1Compact(headers),
			Body:     v1Body(options["body"]),
			Settings: v1Omit(options, "apiKey", "baseURL", "headers", "body"),
		}
	},
	Request: func(options optionsMap) map[string]any {
		generationConfig := v1Pick(options, "thinkingConfig", "responseModalities", "mediaResolution", "imageConfig")
		result := v1Omit(options, "thinkingConfig", "responseModalities", "mediaResolution", "imageConfig")
		if len(generationConfig) > 0 {
			result["generationConfig"] = generationConfig
		}
		return result
	},
}

var v1AzureLowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		headers := map[string]*string{
			"api-key": v1String(options["apiKey"]),
		}
		for key, value := range v1HeaderMap(options["headers"]) {
			value := value
			headers[key] = &value
		}
		return V1ProviderOptionsResult{
			URL:      v1String(options["baseURL"]),
			Headers:  v1Compact(headers),
			Body:     v1Body(options["body"]),
			Settings: v1Omit(options, "apiKey", "baseURL", "headers", "body"),
		}
	},
	Request: v1OpenAILowerer.Request,
}

var v1BedrockLowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		return v1Direct(options)
	},
	Request: func(options optionsMap) map[string]any {
		return map[string]any{"additionalModelRequestFields": v1Clone(options)}
	},
}

var v1OpenAICompatibleLowerer = V1ProviderOptionsLowerer{
	Provider: func(options optionsMap) V1ProviderOptionsResult {
		result := v1Direct(options, "baseURL")
		result.URL = v1String(options["baseURL"])
		return result
	},
	Request: func(options optionsMap) map[string]any {
		result := v1Clone(options)
		if options["reasoningEffort"] != nil {
			result["reasoning_effort"] = options["reasoningEffort"]
			delete(result, "reasoningEffort")
		}
		return result
	},
}

var v1Lowerers = map[string]V1ProviderOptionsLowerer{
	"@ai-sdk/openai":                  v1OpenAILowerer,
	"@ai-sdk/anthropic":               v1AnthropicLowerer,
	"@ai-sdk/google-vertex/anthropic": v1AnthropicLowerer,
	"@ai-sdk/google":                  v1GoogleLowerer,
	"@ai-sdk/google-vertex":           v1GoogleLowerer,
	"@ai-sdk/azure":                   v1AzureLowerer,
	"@ai-sdk/amazon-bedrock":          v1BedrockLowerer,
	"@ai-sdk/openai-compatible":       v1OpenAICompatibleLowerer,
	"@ai-sdk/cerebras":                v1OpenAICompatibleLowerer,
	"@ai-sdk/deepinfra":               v1OpenAICompatibleLowerer,
	"@ai-sdk/groq":                    v1OpenAICompatibleLowerer,
	"@ai-sdk/mistral":                 v1OpenAICompatibleLowerer,
	"@ai-sdk/togetherai":              v1OpenAICompatibleLowerer,
	"@ai-sdk/xai":                     v1OpenAICompatibleLowerer,
	"@openrouter/ai-sdk-provider":     v1OpenAICompatibleLowerer,
	"ai-gateway-provider":             v1OpenAICompatibleLowerer,
	"venice-ai-sdk-provider":          v1OpenAICompatibleLowerer,
}

// V1ProviderOptionsGet mirrors ConfigProviderOptionsV1.get.
func V1ProviderOptionsGet(packageName *string) V1ProviderOptionsLowerer {
	key := ""
	if packageName != nil {
		key = *packageName
	}
	if lowerer, ok := v1Lowerers[key]; ok {
		return lowerer
	}
	return v1RawLowerer
}

func v1Direct(options optionsMap, extraKeys ...string) V1ProviderOptionsResult {
	omitKeys := append([]string{"headers", "body"}, extraKeys...)
	return V1ProviderOptionsResult{
		Headers:  v1HeaderMap(options["headers"]),
		Body:     v1Body(options["body"]),
		Settings: v1Omit(options, omitKeys...),
	}
}

func v1Body(input any) map[string]any {
	record, ok := input.(optionsMap)
	if !ok {
		return nil
	}
	return v1Clone(record)
}

func v1Snake(options optionsMap) map[string]any {
	result := make(map[string]any, len(options))
	for key, value := range options {
		result[v1SnakeKey(key)] = v1SnakeValue(value)
	}
	return result
}

func v1SnakeValue(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = v1SnakeValue(item)
		}
		return result
	case optionsMap:
		result := make(optionsMap, len(typed))
		for key, item := range typed {
			result[v1SnakeKey(key)] = v1SnakeValue(item)
		}
		return result
	default:
		return value
	}
}

func v1SnakeKey(key string) string {
	var builder strings.Builder
	for _, r := range key {
		if r >= 'A' && r <= 'Z' {
			builder.WriteByte('_')
			builder.WriteRune(r + ('a' - 'A'))
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func v1Clone(options optionsMap) map[string]any {
	result := make(map[string]any, len(options))
	for key, value := range options {
		result[key] = value
	}
	return result
}

func v1Omit(options optionsMap, keys ...string) map[string]any {
	excluded := make(map[string]bool, len(keys))
	for _, key := range keys {
		excluded[key] = true
	}
	result := map[string]any{}
	for key, value := range options {
		if excluded[key] {
			continue
		}
		result[key] = value
	}
	return result
}

func v1Pick(options optionsMap, keys ...string) map[string]any {
	included := make(map[string]bool, len(keys))
	for _, key := range keys {
		included[key] = true
	}
	result := map[string]any{}
	for key, value := range options {
		if included[key] {
			result[key] = value
		}
	}
	return result
}

func v1HeaderMap(input any) map[string]string {
	record, ok := input.(optionsMap)
	if !ok {
		return nil
	}
	result := map[string]string{}
	for key, value := range record {
		if text, ok := value.(string); ok {
			result[key] = text
		}
	}
	return result
}

func v1Compact(values map[string]*string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		if value != nil {
			result[key] = *value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func v1CompactUnknown(values map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range values {
		if value != nil {
			result[key] = value
		}
	}
	return result
}

func v1String(input any) *string {
	text, ok := input.(string)
	if !ok || text == "" {
		return nil
	}
	return &text
}

func v1Bearer(input any) *string {
	text, ok := input.(string)
	if !ok || text == "" {
		return nil
	}
	value := "Bearer " + text
	return &value
}

func v1Record(input any) map[string]any {
	if record, ok := input.(optionsMap); ok {
		return v1Clone(record)
	}
	return map[string]any{}
}
