package utils

import "github.com/user/go-agent-sdk/llm/schema"

var OpenAIReasoningEfforts = []string{
	"none", "minimal", "low", "medium", "high", "xhigh",
}

var OpenAIResponseIncludables = []string{
	"file_search_call.results",
	"web_search_call.results",
	"web_search_call.action.sources",
	"message.input_image.image_url",
	"computer_call_output.output.image_url",
	"code_interpreter_call.outputs",
	"reasoning.encrypted_content",
	"message.output_text.logprobs",
}

var OpenAIServiceTiers = []string{"auto", "default", "flex", "priority"}

var reasoningEffortSet = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {},
}

var reasoningEffortsSet = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

// IsReasoningEffort reports whether an effort is supported by OpenAI's API.
func IsReasoningEffort(value string) bool {
	_, ok := reasoningEffortSet[value]
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

func Store(request *schema.LLMRequest) *bool {
	v, ok := openAIOption[bool](request, "store")
	if !ok {
		return nil
	}
	return &v
}

func ReasoningEffort(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "reasoningEffort")
	if !ok {
		return nil
	}
	if _, valid := reasoningEffortsSet[v]; !valid {
		return nil
	}
	return &v
}

func ReasoningSummary(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "reasoningSummary")
	if !ok || v != "auto" {
		return nil
	}
	return &v
}

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

func PromptCacheKey(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "promptCacheKey")
	if !ok {
		return nil
	}
	return &v
}

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

func Instructions(request *schema.LLMRequest) *string {
	v, ok := openAIOption[string](request, "instructions")
	if !ok {
		return nil
	}
	return &v
}
