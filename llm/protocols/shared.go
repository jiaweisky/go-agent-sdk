package protocols

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/go-agent-sdk/llm/schema"
)

type ToolAccumulator struct {
	ID    string
	Name  string
	Input string
}

func TotalTokens(inputTokens, outputTokens, total *int) *int {
	if total != nil {
		return total
	}
	if inputTokens == nil && outputTokens == nil {
		return nil
	}
	in := 0
	if inputTokens != nil {
		in = *inputTokens
	}
	out := 0
	if outputTokens != nil {
		out = *outputTokens
	}
	sum := in + out
	return &sum
}

func SubtractTokens(total, subtrahend *int) *int {
	if total == nil {
		return nil
	}
	if subtrahend == nil {
		return total
	}
	diff := *total - *subtrahend
	if diff < 0 {
		diff = 0
	}
	return &diff
}

func SumTokens(values ...*int) *int {
	allNil := true
	sum := 0
	for _, v := range values {
		if v != nil {
			allNil = false
			sum += *v
		}
	}
	if allNil {
		return nil
	}
	return &sum
}

func JoinText(parts []struct{ Text string }) string {
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.Text
	}
	return strings.Join(texts, "\n")
}

func escapeSystemUpdateText(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func WrapSystemUpdate(parts []struct{ Text string }) string {
	return "<system-update>\n" + escapeSystemUpdateText(JoinText(parts)) + "\n</system-update>"
}

func TrimBaseURL(value string) string {
	return strings.TrimRight(value, "/")
}

func ToolResultText(part schema.ToolResultPart) string {
	switch part.Result.Type {
	case "text":
		return fmt.Sprint(part.Result.Value)
	case "error":
		return fmt.Sprint(part.Result.Value)
	default:
		b, _ := json.Marshal(part.Result.Value)
		return string(b)
	}
}

func ErrorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return "Unknown stream error"
}

func ParseToolInput(route, name, raw string) (any, error) {
	input := raw
	if input == "" {
		input = "{}"
	}
	var result any
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		return nil, fmt.Errorf("Invalid JSON input for %s tool call %s: %w", route, name, err)
	}
	return result, nil
}

func EventError(route, message string, raw ...string) *schema.LLMError {
	opts := []schema.ReasonOption{schema.WithRoute(route)}
	if len(raw) > 0 {
		opts = append(opts, schema.WithRaw(raw[0]))
	}
	return &schema.LLMError{
		Module: "ProviderShared",
		Method: "stream",
		Reason: schema.NewInvalidProviderOutputReason(message, opts...),
	}
}

func InvalidRequest(message string) *schema.LLMError {
	return &schema.LLMError{
		Module: "ProviderShared",
		Method: "request",
		Reason: schema.NewInvalidRequestReason(message),
	}
}

var ImageMIMEs = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
var VideoMIMEs = []string{"video/mp4", "video/webm", "video/quicktime"}
var AudioMIMEs = []string{"audio/wav", "audio/mp3", "audio/aiff", "audio/aac", "audio/ogg", "audio/flac"}
var MediaMIMEs = append(append([]string{}, ImageMIMEs...), append(VideoMIMEs, AudioMIMEs...)...)

func SupportsContent(part schema.ContentPart, types []string) bool {
	switch part.(type) {
	case schema.TextPart:
		for _, t := range types {
			if t == "text" {
				return true
			}
		}
	case schema.MediaPart:
		for _, t := range types {
			if t == "media" {
				return true
			}
		}
	case schema.ToolCallPart:
		for _, t := range types {
			if t == "tool-call" {
				return true
			}
		}
	case schema.ToolResultPart:
		for _, t := range types {
			if t == "tool-result" {
				return true
			}
		}
	case schema.ReasoningPart:
		for _, t := range types {
			if t == "reasoning" {
				return true
			}
		}
	}
	return false
}

func FormatContentTypes(types []string) string {
	if len(types) == 0 {
		return ""
	}
	if len(types) == 1 {
		return types[0]
	}
	if len(types) == 2 {
		return types[0] + " and " + types[1]
	}
	return strings.Join(types[:len(types)-1], ", ") + ", and " + types[len(types)-1]
}

func UnsupportedContent(route string, role schema.MessageRole, types []string) *schema.LLMError {
	return InvalidRequest(fmt.Sprintf("%s %s messages only support %s content for now", route, role, FormatContentTypes(types)))
}

func MatchToolChoice(route string, toolChoice schema.ToolChoice, cases struct {
	Auto     func() any
	None     func() any
	Required func() any
	Tool     func(name string) any
}) (any, error) {
	switch toolChoice.Type {
	case "auto":
		return cases.Auto(), nil
	case "none":
		return cases.None(), nil
	case "required":
		return cases.Required(), nil
	case "tool":
		if toolChoice.Name == nil || *toolChoice.Name == "" {
			return nil, InvalidRequest(fmt.Sprintf("%s tool choice requires a tool name", route))
		}
		return cases.Tool(*toolChoice.Name), nil
	default:
		return nil, InvalidRequest(fmt.Sprintf("%s unknown tool choice type: %s", route, toolChoice.Type))
	}
}
