package schema

import (
	"errors"
	"regexp"
)

// Context-overflow detection lives in this leaf package because both the llm
// root package (provider_error.go) and llm/route (executor.go) need it, and
// route cannot import llm (llm imports route).

var contextOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)request_too_large`),
	regexp.MustCompile(`(?i)input is too long for requested model`),
	regexp.MustCompile(`(?i)exceeds the context window`),
	regexp.MustCompile(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length(?: of [\d,]+ tokens?|\s*\([\d,]+\))`),
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),
	regexp.MustCompile(`(?i)tokens in request more than max tokens allowed`),
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),
	regexp.MustCompile(`(?i)reduce the length of the messages`),
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),
	regexp.MustCompile(`(?i)exceeds (?:the )?maximum allowed input length of [\d,]+ tokens?`),
	regexp.MustCompile(`(?i)input \(\d+ tokens\) is longer than the model'?s context length \(\d+ tokens\)`),
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),
	regexp.MustCompile(`(?i)exceeds the available context size`),
	regexp.MustCompile(`(?i)greater than the context length`),
	regexp.MustCompile(`(?i)context window exceeds limit`),
	regexp.MustCompile(`(?i)exceeded model token limit`),
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
	regexp.MustCompile(`(?i)request entity too large`),
	regexp.MustCompile(`(?i)context length is only \d+ tokens`),
	regexp.MustCompile(`(?i)input length.*exceeds.*context length`),
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),
	regexp.MustCompile(`(?i)prompt has [\d,]+ tokens?, but the configured context size is [\d,]+ tokens?`),
	regexp.MustCompile(`(?i)model_context_window_exceeded`),
	regexp.MustCompile(`(?i)too many tokens`),
	regexp.MustCompile(`(?i)token limit exceeded`),
}

var contextOverflowExclusions = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(throttling error|service unavailable):`),
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
}

var contextOverflowNoBody = regexp.MustCompile(`(?i)^4(00|13)\s*(status code)?\s*\(no body\)`)

// IsContextOverflow reports whether a provider error message indicates the
// request exceeded the model's context window.
func IsContextOverflow(message string) bool {
	for _, excl := range contextOverflowExclusions {
		if excl.MatchString(message) {
			return false
		}
	}
	for _, pat := range contextOverflowPatterns {
		if pat.MatchString(message) {
			return true
		}
	}
	return contextOverflowNoBody.MatchString(message)
}

// IsContextOverflowFailure reports whether err is an LLMError classified as a
// context overflow.
func IsContextOverflowFailure(err error) bool {
	var e *LLMError
	if !errors.As(err, &e) {
		return false
	}
	return e.Reason.Kind == ReasonInvalidRequest &&
		e.Reason.Classification != nil &&
		*e.Reason.Classification == ClassificationContextOverflow
}
