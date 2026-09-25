package utils

import "github.com/user/go-agent-sdk/llm/schema"

// openAIReasoningEffortSet is the efforts IsReasoningEffort accepts.
var openAIReasoningEffortSet = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {},
}

// openAIReasoningEffortWithMaxSet additionally accepts "max": ReasoningEffort
// extracts it from the request, but IsReasoningEffort rejects it downstream.
var openAIReasoningEffortWithMaxSet = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

// IsReasoningEffort reports whether an effort is supported by OpenAI's API.
func IsReasoningEffort(value string) bool {
	_, ok := openAIReasoningEffortSet[value]
	return ok
}

var textVerbositySet = map[string]struct{}{
	"low": {}, "medium": {}, "high": {},
}

var includablesSet = map[string]struct{}{
	"file_search_call.results":              {},
	"web_search_call.results":               {},
	"web_search_call.action.sources":        {},
	"message.input_image.image_url":         {},
	"computer_call_output.output.image_url": {},
	"code_interpreter_call.outputs":         {},
	"reasoning.encrypted_content":           {},
	"message.output_text.logprobs":          {},
}

var serviceTiersSet = map[string]struct{}{
	"auto": {}, "default": {}, "flex": {}, "priority": {},
}

func openAIOptions(request *schema.LLMRequest) map[string]any {
	if request.ProviderOptions == nil {
		return nil
	}
	return request.ProviderOptions["openai"]
}

// openAIOption returns the request's openai provider option for key, when the
// request carries one of the expected type.
func openAIOption[T any](request *schema.LLMRequest, key string) (T, bool) {
	opts := openAIOptions(request)
	if opts == nil {
		var zero T
		return zero, false
	}
	value, ok := opts[key].(T)
	return value, ok
}

// Store returns the request's "store" option, if set.
func Store(request *schema.LLMRequest) *bool {
	v, ok := openAIOption[bool](request, "store")
	if !ok {
		return nil
	}
	return &v
}

// ReasoningEffort returns the request's reasoning effort, if set and supported.
func ReasoningEffort(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "reasoningEffort")
	if !ok {
		return nil
	}
	if _, valid := openAIReasoningEffortWithMaxSet[v]; !valid {
		return nil
	}
	return &v
}

// ReasoningSummary returns the request's reasoning summary, when it is "auto".
func ReasoningSummary(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "reasoningSummary")
	if !ok || v != "auto" {
		return nil
	}
	return &v
}

// Include returns the request's include list, filtered to the supported values.
func Include(request *schema.LLMRequest) []string {
	raw, ok := openAIOption[[]any](request, "include")
	if !ok {
		return nil
	}
	filtered := make([]string, 0, len(raw))
	for _, entry := range raw {
		s, ok := entry.(string)
		if !ok {
			continue
		}
		if _, valid := includablesSet[s]; valid {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// PromptCacheKey returns the request's prompt cache key, if set.
func PromptCacheKey(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "promptCacheKey")
	if !ok {
		return nil
	}
	return &v
}

// TextVerbosity returns the request's text verbosity, if set and supported.
func TextVerbosity(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "textVerbosity")
	if !ok {
		return nil
	}
	if _, valid := textVerbositySet[v]; !valid {
		return nil
	}
	return &v
}

// ServiceTier returns the request's service tier, if set and supported.
func ServiceTier(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "serviceTier")
	if !ok {
		return nil
	}
	if _, valid := serviceTiersSet[v]; !valid {
		return nil
	}
	return &v
}

// Instructions returns the request's instructions, if set.
func Instructions(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "instructions")
	if !ok {
		return nil
	}
	return &v
}
