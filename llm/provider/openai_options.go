package provider

import (
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// OpenAIOptionsInput is the ergonomic form of the "openai" provider options.
type OpenAIOptionsInput struct {
	Store            *bool    `json:"store,omitempty"`
	PromptCacheKey   *string  `json:"promptCacheKey,omitempty"`
	ReasoningEffort  *string  `json:"reasoningEffort,omitempty"`
	ReasoningSummary *string  `json:"reasoningSummary,omitempty"`
	Include          []string `json:"include,omitempty"`
	TextVerbosity    *string  `json:"textVerbosity,omitempty"`
	ServiceTier      *string  `json:"serviceTier,omitempty"`
}

func openAIProviderOptions(opts *OpenAIOptionsInput) schema.ProviderOptions {
	if opts == nil {
		return nil
	}
	openai := make(map[string]any)
	if opts.Store != nil {
		openai["store"] = *opts.Store
	}
	if opts.PromptCacheKey != nil {
		openai["promptCacheKey"] = *opts.PromptCacheKey
	}
	if opts.ReasoningEffort != nil {
		openai["reasoningEffort"] = *opts.ReasoningEffort
	}
	if opts.ReasoningSummary != nil {
		openai["reasoningSummary"] = *opts.ReasoningSummary
	}
	if len(opts.Include) > 0 {
		openai["include"] = opts.Include
	}
	if opts.TextVerbosity != nil {
		openai["textVerbosity"] = *opts.TextVerbosity
	}
	if opts.ServiceTier != nil {
		openai["serviceTier"] = *opts.ServiceTier
	}
	if len(openai) == 0 {
		return nil
	}
	return schema.ProviderOptions{"openai": openai}
}

func openAIGPT5DefaultOptions(modelID string, textVerbosity *bool) schema.ProviderOptions {
	id := strings.ToLower(modelID)
	if !strings.Contains(id, "gpt-5") ||
		strings.Contains(id, "gpt-5-chat") ||
		strings.Contains(id, "gpt-5-pro") {
		return nil
	}
	opts := OpenAIOptionsInput{
		ReasoningEffort:  ptr("medium"),
		ReasoningSummary: ptr("auto"),
		Include:          []string{"reasoning.encrypted_content"},
	}
	if textVerbosity != nil && *textVerbosity &&
		strings.Contains(id, "gpt-5.") &&
		!strings.Contains(id, "codex") &&
		!strings.Contains(id, "-chat") {
		v := "low"
		opts.TextVerbosity = &v
	}
	return openAIProviderOptions(&opts)
}

func openAIDefaultOptions(modelID string, textVerbosity *bool) schema.ProviderOptions {
	return schema.MergeProviderOptions(
		openAIProviderOptions(&OpenAIOptionsInput{Store: ptr(false)}),
		openAIGPT5DefaultOptions(modelID, textVerbosity),
	)
}

func withOpenAIOptions(
	modelID string,
	providerOptions schema.ProviderOptions,
	textVerbosity *bool,
) schema.ProviderOptions {
	return schema.MergeProviderOptions(openAIDefaultOptions(modelID, textVerbosity), providerOptions)
}

func ptr[T any](v T) *T {
	return &v
}
