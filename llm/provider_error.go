package llm

import "github.com/jiaweisky/go-agent-sdk/llm/schema"

// IsContextOverflow reports whether a provider error message indicates the
// request exceeded the model's context window. The canonical implementation
// lives in llm/schema (a leaf importable by both llm and llm/route).
func IsContextOverflow(message string) bool { return schema.IsContextOverflow(message) }

// IsContextOverflowFailure reports whether err is an LLMError classified as a
// context overflow.
func IsContextOverflowFailure(err error) bool { return schema.IsContextOverflowFailure(err) }
