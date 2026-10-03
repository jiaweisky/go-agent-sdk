package session

import (
	"regexp"
	"strings"
)

// TitleSettings controls automatic session title generation.
type TitleSettings struct {
	// Auto enables title generation.
	Auto bool
	// Model overrides the model used to generate titles, taking priority over
	// the title agent's own model. Empty falls back to the title agent's model
	// and then the session model.
	Model string
}

// DefaultTitleSettings is the built-in title configuration: disabled.
func DefaultTitleSettings() TitleSettings {
	return TitleSettings{}
}

// TitlePromptPrefix precedes the first user message when asking the model for a
// title.
const TitlePromptPrefix = "Generate a title for this conversation:\n"

// titleMaxLength caps the stored title; longer ones keep titleMaxLength-3 runes
// plus a trailing "...".
const titleMaxLength = 100

var (
	defaultTitlePattern = regexp.MustCompile(`^(New session|Child session) - \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	thinkBlockPattern   = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)
)

// IsDefaultTitle reports whether a title is still the placeholder the SDK
// assigns at creation (or empty, which means unnamed). Title generation only
// runs while the title is default, so it never clobbers a user-provided or
// already-generated title.
func IsDefaultTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	return trimmed == "" || defaultTitlePattern.MatchString(trimmed)
}

// CleanTitle extracts the title from a model reply: it drops any reasoning
// block, keeps the first non-empty line, and truncates long titles. It returns
// "" when nothing usable remains.
func CleanTitle(text string) string {
	cleaned := thinkBlockPattern.ReplaceAllString(text, "")
	for _, line := range strings.Split(cleaned, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > titleMaxLength {
			return string(runes[:titleMaxLength-3]) + "..."
		}
		return line
	}
	return ""
}
