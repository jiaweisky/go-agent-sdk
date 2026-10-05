package schema

import (
	"encoding/json"
	"fmt"
)

// --- Tool Content Types ---

// ToolTextContent is a tool result's text.
type ToolTextContent struct {
	Type string `json:"type"` // must be "text"
	Text string `json:"text"`
}

func (ToolTextContent) toolContentTag() {}

// ToolFileContent is a tool result's file reference.
type ToolFileContent struct {
	Type string  `json:"type"` // must be "file"
	URI  string  `json:"uri"`
	Mime string  `json:"mime"`
	Name *string `json:"name,omitempty"`
}

func (ToolFileContent) toolContentTag() {}

// ToolContent is one piece of a tool result: text or a file.
type ToolContent interface {
	toolContentTag()
}

func toToolContentSlice(v any) []ToolContent {
	switch items := v.(type) {
	case nil:
		return nil
	case []ToolContent:
		return items
	case []ToolTextContent:
		s := make([]ToolContent, len(items))
		for i := range items {
			s[i] = items[i]
		}
		return s
	case []ToolFileContent:
		s := make([]ToolContent, len(items))
		for i := range items {
			s[i] = items[i]
		}
		return s
	default:
		return nil
	}
}

// --- System Part ---

// SystemPart is one part of the system prompt.
type SystemPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Cache    *CacheHint     `json:"cache,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// NewSystemPart builds a text system part.
func NewSystemPart(text string) SystemPart {
	return SystemPart{Type: "text", Text: text}
}

// SystemParts normalizes a string, a part, or a slice of them into system parts.
func SystemParts(input any) []SystemPart {
	switch v := input.(type) {
	case nil:
		return []SystemPart{}
	case string:
		return []SystemPart{NewSystemPart(v)}
	case SystemPart:
		return []SystemPart{v}
	case []SystemPart:
		return v
	default:
		return []SystemPart{}
	}
}

// --- Text Part ---

// TextPart is a text block in a message.
type TextPart struct {
	Type             string           `json:"type"` // must be "text"
	Text             string           `json:"text"`
	Cache            *CacheHint       `json:"cache,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (TextPart) contentPartTag() {}

// --- Media Part ---

// MediaPart is an image or other media block in a message.
type MediaPart struct {
	Type      string         `json:"type"` // must be "media"
	MediaType string         `json:"mediaType"`
	Data      any            `json:"data"`
	Filename  *string        `json:"filename,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (MediaPart) contentPartTag() {}

// --- Reasoning Part ---

// ReasoningPart is a model's reasoning block, with any provider signature.
type ReasoningPart struct {
	Type             string           `json:"type"` // must be "reasoning"
	Text             string           `json:"text"`
	Encrypted        *string          `json:"encrypted,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningPart) contentPartTag() {}

// --- Tool Call Part ---

// ToolCallPart is a tool call the model asked for.
type ToolCallPart struct {
	Type             string           `json:"type"` // must be "tool-call"
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Input            any              `json:"input"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ToolCallPart) contentPartTag() {}

// ToolCallPartInput is the input form of a ToolCallPart.
type ToolCallPartInput struct {
	ID               string
	Name             string
	Input            any
	ProviderExecuted *bool
	Metadata         map[string]any
	ProviderMetadata ProviderMetadata
}

// NewToolCallPart builds a tool call part.
func NewToolCallPart(input ToolCallPartInput) ToolCallPart {
	return ToolCallPart{
		Type:             "tool-call",
		ID:               input.ID,
		Name:             input.Name,
		Input:            input.Input,
		ProviderExecuted: input.ProviderExecuted,
		Metadata:         input.Metadata,
		ProviderMetadata: input.ProviderMetadata,
	}
}

// --- Tool Result Value ---

// ToolResultValue is a tool result: its kind ("json", "text", "error" or
// "content") and its value.
type ToolResultValue struct {
	Type  string `json:"type"` // "json", "text", "error", "content"
	Value any    `json:"value"`
}

// isToolResultValue reports whether v looks like a tool result value.
func isToolResultValue(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		_, ok = v.(ToolResultValue)
		return ok
	}
	t, _ := m["type"].(string)
	switch t {
	case "json", "text", "error", "content":
		_, hasValue := m["value"]
		return hasValue
	}
	return false
}

// AsToolResultValue returns v as a ToolResultValue when it already is one or is a
// result-shaped map.
func AsToolResultValue(v any) (ToolResultValue, bool) {
	if rv, ok := v.(ToolResultValue); ok {
		return rv, true
	}
	if isToolResultValue(v) {
		m := v.(map[string]any)
		t, _ := m["type"].(string)
		return ToolResultValue{Type: t, Value: m["value"]}, true
	}
	return ToolResultValue{}, false
}

// NewToolResultValue builds a tool result value, inferring its kind from the Go
// type unless typ overrides it.
func NewToolResultValue(value any, typ ...string) ToolResultValue {
	if rv, ok := AsToolResultValue(value); ok {
		return rv
	}
	t := "json"
	if len(typ) > 0 {
		t = typ[0]
	}
	if t == "content" {
		arr := toToolContentSlice(value)
		if arr == nil {
			arr = []ToolContent{}
		}
		return ToolResultValue{Type: t, Value: arr}
	}
	return ToolResultValue{Type: t, Value: value}
}

// --- Tool Result Part ---

// ToolResultPart is a tool result sent back to the model.
type ToolResultPart struct {
	Type             string           `json:"type"` // must be "tool-result"
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Result           ToolResultValue  `json:"result"`
	ProviderExecuted *bool            `json:"providerExecuted,omitempty"`
	Cache            *CacheHint       `json:"cache,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ToolResultPart) contentPartTag() {}

// ToolResultPartInput is the input form of a ToolResultPart.
type ToolResultPartInput struct {
	ID               string
	Name             string
	Result           any
	ResultType       string
	ProviderExecuted *bool
	Cache            *CacheHint
	Metadata         map[string]any
	ProviderMetadata ProviderMetadata
}

// NewToolResultPart builds a tool result part.
func NewToolResultPart(input ToolResultPartInput) ToolResultPart {
	return ToolResultPart{
		Type:             "tool-result",
		ID:               input.ID,
		Name:             input.Name,
		Result:           NewToolResultValue(input.Result, input.ResultType),
		ProviderExecuted: input.ProviderExecuted,
		Cache:            input.Cache,
		Metadata:         input.Metadata,
		ProviderMetadata: input.ProviderMetadata,
	}
}

// --- Tool Output ---

// ToolOutput is what a tool produced: the structured value and the content the
// model sees.
type ToolOutput struct {
	Structured any           `json:"structured"`
	Content    []ToolContent `json:"content"`
}

// NewToolOutput builds a tool output.
func NewToolOutput(structured any, content ...ToolContent) ToolOutput {
	return ToolOutput{Structured: structured, Content: content}
}

// ToolOutputFromResultValue converts a tool result value into a tool output.
func ToolOutputFromResultValue(result ToolResultValue) *ToolOutput {
	switch result.Type {
	case "json":
		return &ToolOutput{Structured: result.Value, Content: []ToolContent{}}
	case "text":
		return &ToolOutput{
			Structured: map[string]any{},
			Content:    []ToolContent{ToolTextContent{Type: "text", Text: toolResultText(result.Value)}},
		}
	case "content":
		content := toToolContentSlice(result.Value)
		if content == nil {
			content = []ToolContent{}
		}
		return &ToolOutput{Structured: map[string]any{}, Content: content}
	case "error":
		return nil
	default:
		return nil
	}
}

// ToolOutputToResultValue converts a tool output into a tool result value.
func ToolOutputToResultValue(output ToolOutput) ToolResultValue {
	if len(output.Content) == 0 {
		return ToolResultValue{Type: "json", Value: output.Structured}
	}
	if len(output.Content) == 1 {
		switch c := output.Content[0].(type) {
		case ToolTextContent:
			return ToolResultValue{Type: "text", Value: c.Text}
		case *ToolTextContent:
			return ToolResultValue{Type: "text", Value: c.Text}
		}
	}
	return ToolResultValue{Type: "content", Value: output.Content}
}

func toolResultText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// --- Content Part ---

// ContentPart is one block of a message: text, media, reasoning or a tool call.
type ContentPart interface {
	contentPartTag()
}

// --- Response Format ---

// ResponseFormatText asks for plain text.
type ResponseFormatText struct {
	Type string `json:"type"` // "text"
}

func (ResponseFormatText) responseFormatTag() {}

// ResponseFormatJSON asks for JSON matching a schema.
type ResponseFormatJSON struct {
	Type   string     `json:"type"` // "json"
	Schema JsonSchema `json:"schema"`
}

func (ResponseFormatJSON) responseFormatTag() {}

// ResponseFormatTool asks for the arguments of a named tool.
type ResponseFormatTool struct {
	Type string         `json:"type"` // "tool"
	Tool ToolDefinition `json:"tool"`
}

func (ResponseFormatTool) responseFormatTag() {}

// ResponseFormat is how the response should be shaped.
type ResponseFormat interface {
	responseFormatTag()
}

// --- Tool Definition ---

// ToolDefinition is a tool as the model sees it.
type ToolDefinition struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  JsonSchema     `json:"inputSchema"`
	OutputSchema *JsonSchema    `json:"outputSchema,omitempty"`
	Cache        *CacheHint     `json:"cache,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Native       map[string]any `json:"native,omitempty"`
}

// ToolDef is an alias for ToolDefinition.
type ToolDef = ToolDefinition

// ToolDefinitionInput is the input form of a ToolDefinition.
type ToolDefinitionInput struct {
	Name         string
	Description  string
	InputSchema  JsonSchema
	OutputSchema *JsonSchema
	Cache        *CacheHint
	Metadata     map[string]any
	Native       map[string]any
}

// NewToolDefinition normalizes a definition, a pointer to one, or its input form.
func NewToolDefinition(input any) ToolDefinition {
	switch v := input.(type) {
	case ToolDefinition:
		return v
	case *ToolDefinition:
		return *v
	case ToolDefinitionInput:
		return ToolDefinition(v)
	default:
		return ToolDefinition{}
	}
}

// --- Tool Choice ---

// ToolChoice is how the model should choose a tool: a mode, or a named tool.
type ToolChoice struct {
	Type string  `json:"type"` // "auto", "none", "required", "tool"
	Name *string `json:"name,omitempty"`
}

// ToolChoiceAuto lets the model decide.
var ToolChoiceAuto = ToolChoice{Type: "auto"}

// ToolChoiceNone forbids tool calls.
var ToolChoiceNone = ToolChoice{Type: "none"}

// ToolChoiceRequired forces at least one tool call.
var ToolChoiceRequired = ToolChoice{Type: "required"}

// ToolChoiceNamed forces one named tool.
func ToolChoiceNamed(name string) ToolChoice {
	return ToolChoice{Type: "tool", Name: &name}
}

// NewToolChoice normalizes a tool choice, a tool definition, or a mode/tool name.
func NewToolChoice(input any) ToolChoice {
	switch v := input.(type) {
	case ToolChoice:
		return v
	case ToolDefinition:
		return ToolChoiceNamed(v.Name)
	case string:
		switch v {
		case "auto", "none", "required":
			return ToolChoice{Type: v}
		default:
			return ToolChoiceNamed(v)
		}
	default:
		return ToolChoiceAuto
	}
}

// --- Message ---

// Message is one turn of the conversation.
type Message struct {
	ID       string         `json:"id,omitempty"`
	Role     MessageRole    `json:"role"`
	Content  []ContentPart  `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Native   map[string]any `json:"native,omitempty"`
}

// NewTextContent builds a text content part.
func NewTextContent(value string) ContentPart {
	return TextPart{Type: "text", Text: value}
}

// MessageContent normalizes a string, a part, or a slice of them into content
// parts.
func MessageContent(input any) []ContentPart {
	switch v := input.(type) {
	case nil:
		return []ContentPart{}
	case string:
		return []ContentPart{NewTextContent(v)}
	case ContentPart:
		return []ContentPart{v}
	case []ContentPart:
		if v == nil {
			return []ContentPart{}
		}
		return v
	default:
		return []ContentPart{}
	}
}

// MessageInput is the input form of a Message.
type MessageInput struct {
	ID       string
	Role     MessageRole
	Content  any // string | ContentPart | []ContentPart
	Metadata map[string]any
	Native   map[string]any
}

// NewMessage normalizes a message or its input form.
func NewMessage(input any) Message {
	switch v := input.(type) {
	case Message:
		return v
	case *Message:
		return *v
	case MessageInput:
		return Message{
			ID:       v.ID,
			Role:     v.Role,
			Content:  MessageContent(v.Content),
			Metadata: v.Metadata,
			Native:   v.Native,
		}
	default:
		return Message{}
	}
}

// MessageUser builds a user message.
func MessageUser(content any) Message {
	return NewMessage(MessageInput{Role: RoleUser, Content: content})
}

// MessageAssistant builds an assistant message.
func MessageAssistant(content any) Message {
	return NewMessage(MessageInput{Role: RoleAssistant, Content: content})
}

// MessageSystem builds a system message, an instruction at this point in the
// conversation rather than the initial system prompt.
func MessageSystem(content any) Message {
	return NewMessage(MessageInput{Role: RoleSystem, Content: content})
}

// MessageTool builds a tool-result message.
func MessageTool(result any) Message {
	switch v := result.(type) {
	case ToolResultPart:
		return NewMessage(MessageInput{Role: RoleTool, Content: []ContentPart{v}})
	default:
		input, ok := result.(ToolResultPartInput)
		if ok {
			return NewMessage(MessageInput{Role: RoleTool, Content: []ContentPart{NewToolResultPart(input)}})
		}
		return Message{}
	}
}

// --- LLM Request ---

// LLMRequest is a fully resolved request: the model, the conversation, the
// tools, and the options for this call.
type LLMRequest struct {
	ID              string             `json:"id,omitempty"`
	Model           ModelSchema        `json:"model"`
	System          []SystemPart       `json:"system"`
	Messages        []Message          `json:"messages"`
	Tools           []ToolDefinition   `json:"tools"`
	ToolChoice      *ToolChoice        `json:"toolChoice,omitempty"`
	Generation      *GenerationOptions `json:"generation,omitempty"`
	ProviderOptions ProviderOptions    `json:"providerOptions,omitempty"`
	Http            *HttpOptions       `json:"http,omitempty"`
	ResponseFormat  ResponseFormat     `json:"responseFormat,omitempty"`
	Cache           *CachePolicy       `json:"cache,omitempty"`
	Metadata        map[string]any     `json:"metadata,omitempty"`
}

// Clone returns a shallow copy of the request: slices, maps and pointers are
// shared, so callers replace a field rather than mutate it in place.
func (r *LLMRequest) Clone() *LLMRequest {
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}
