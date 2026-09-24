package protocols

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/go-agent-sdk/llm/protocols/utils"
	"github.com/user/go-agent-sdk/llm/schema"
)

// schemaMap normalizes a JSON schema held as `any`. ToolDefinition.InputSchema
// has the named type JsonSchema, so a plain `.(map[string]any)` assertion fails
// and would silently drop the schema.
func schemaMap(inputSchema any) map[string]any {
	switch v := inputSchema.(type) {
	case map[string]any:
		return v
	case schema.JsonSchema:
		return v
	}
	return nil
}

// ToolSchemaCompatibility mirrors request.model.compatibility?.toolSchema.
func ToolSchemaCompatibility(model schema.ModelSchema) string {
	compatibility := model.GetCompatibility()
	if compatibility == nil || compatibility.ToolSchema == nil {
		return ""
	}
	return string(*compatibility.ToolSchema)
}

// ProjectToolSchema mirrors ToolSchemaProjection.modelCompatibility.
func ProjectToolSchema(model schema.ModelSchema, inputSchema any) any {
	return projectToolSchema(inputSchema, ToolSchemaCompatibility(model))
}

func projectToolSchema(inputSchema any, compatibility string) any {
	if compatibility == "" {
		return inputSchema
	}
	s := schemaMap(inputSchema)
	if s == nil {
		return inputSchema
	}
	return utils.ModelCompatibility(s, compatibility)
}

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

// PartText extracts the text of a content part for system-update lowering.
// Content parts are structs, so formatting them directly would emit Go struct
// dumps; only text/reasoning parts carry text.
func PartText(part schema.ContentPart) string {
	switch v := part.(type) {
	case schema.TextPart:
		return v.Text
	case *schema.TextPart:
		if v != nil {
			return v.Text
		}
	case schema.ReasoningPart:
		return v.Text
	case *schema.ReasoningPart:
		if v != nil {
			return v.Text
		}
	}
	return ""
}

// SystemUpdateParts lowers a system message's content parts to text parts.
func SystemUpdateParts(content []schema.ContentPart) []struct{ Text string } {
	parts := make([]struct{ Text string }, len(content))
	for i := range content {
		parts[i].Text = PartText(content[i])
	}
	return parts
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

// ParseToolInput decodes a tool call's JSON input. The canonical implementation
// lives in llm/protocols/utils (the leaf package that both this package and
// tool-stream can import); this re-exports it.
func ParseToolInput(route, name, raw string) (any, error) {
	return utils.ParseToolInput(route, name, raw)
}

// EventError builds the shared invalid-provider-output error.
func EventError(route, message string, raw ...string) *schema.LLMError {
	return utils.EventError(route, message, raw...)
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

// mediaMIMESet builds a lookup set from a MIME list.
func mediaMIMESet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

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

// MatchToolChoice mirrors ProviderShared.matchToolChoice: the mode choices map
// to their case and any other type is a named tool, which requires a name.
func MatchToolChoice(route string, toolChoice schema.ToolChoice, auto, none, required any, tool func(name string) any) (any, error) {
	switch toolChoice.Type {
	case "auto":
		return auto, nil
	case "none":
		return none, nil
	case "required":
		return required, nil
	}
	if toolChoice.Name == nil || *toolChoice.Name == "" {
		return nil, InvalidRequest(fmt.Sprintf("%s tool choice requires a tool name", route))
	}
	return tool(*toolChoice.Name), nil
}
