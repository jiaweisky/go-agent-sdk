package protocols

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/llm/protocols/utils"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
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

// ToolSchemaCompatibility returns the tool-schema shape the model requires, or
// "" when it has none.
func ToolSchemaCompatibility(model schema.ModelSchema) string {
	compatibility := model.GetCompatibility()
	if compatibility == nil || compatibility.ToolSchema == nil {
		return ""
	}
	return string(*compatibility.ToolSchema)
}

// ProjectToolSchema rewrites a tool schema into the shape the model requires.
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

// toolParameters normalizes a tool schema into the shape the OpenAI protocols
// send: a top-level object with flattened `anyOf` variants.
func toolParameters(inputSchema any) map[string]any {
	params := schemaMap(inputSchema)
	if params == nil {
		return nil
	}
	return utils.ToolSchemaOpenAI(params)
}

// TotalTokens returns the total when the provider reports it, and the sum of the
// input and output tokens otherwise.
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

// JoinText joins the parts' texts with a newline.
func JoinText(parts []struct{ Text string }) string {
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.Text
	}
	return strings.Join(texts, "\n")
}

// partText extracts the text of a content part for system-update lowering.
// Content parts are structs, so formatting them directly would emit Go struct
// dumps; only text/reasoning parts carry text.
func partText(part schema.ContentPart) string {
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
		parts[i].Text = partText(content[i])
	}
	return parts
}

// SystemUpdateCache returns the cache hint of a system update's last text part,
// or nil.
func SystemUpdateCache(content []schema.ContentPart) *schema.CacheHint {
	if len(content) == 0 {
		return nil
	}
	if tp, ok := content[len(content)-1].(schema.TextPart); ok {
		return tp.Cache
	}
	return nil
}

func escapeSystemUpdateText(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

// WrapSystemUpdate wraps a mid-conversation instruction so the model does not read
// it as the initial system prompt.
func WrapSystemUpdate(parts []struct{ Text string }) string {
	return "<system-update>\n" + escapeSystemUpdateText(JoinText(parts)) + "\n</system-update>"
}

// ToolResultText renders a tool result part as text.
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

// InvalidRequest builds an invalid-request error for a route.
func InvalidRequest(message string) *schema.LLMError {
	return &schema.LLMError{
		Module: "ProviderShared",
		Method: "request",
		Reason: schema.NewInvalidRequestReason(message),
	}
}

// imageMIMEs are the image types the protocols accept.
var imageMIMEs = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// videoMIMEs are the video types the protocols accept.
var videoMIMEs = []string{"video/mp4", "video/webm", "video/quicktime"}

// audioMIMEs are the audio types the protocols accept.
var audioMIMEs = []string{"audio/wav", "audio/mp3", "audio/aiff", "audio/aac", "audio/ogg", "audio/flac"}

// mediaMIMEs are every media type the protocols accept.
var mediaMIMEs = append(append([]string{}, imageMIMEs...), append(videoMIMEs, audioMIMEs...)...)

// mediaMIMESet builds a lookup set from a MIME list.
func mediaMIMESet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// imageMIMESet is the lookup set of image types the protocols accept.
var imageMIMESet = mediaMIMESet(imageMIMEs)

// intVal dereferences p, or returns 0 when p is nil.
func intVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// providerMetadata keys a raw payload by provider name, or returns nil when the
// payload is nil.
func providerMetadata(provider string, raw map[string]any) schema.ProviderMetadata {
	if raw == nil {
		return nil
	}
	return schema.ProviderMetadata{provider: raw}
}

// rawRecord decodes a JSON object into a map, or returns nil when it is not one.
func rawRecord(data []byte) map[string]any {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}

// strVal dereferences p, or returns def when p is nil and def is given.
func strVal(p *string, def ...string) string {
	if p != nil {
		return *p
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// MatchToolChoice maps a tool choice to the value a protocol sends: the mode
// choices use auto, none and required, and any other type is a named tool, which
// needs a name.
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
