package schema

import (
	"encoding/json"
	"fmt"
)

// --- Tool Content Types ---

type ToolTextContent struct {
	Type string `json:"type"` // must be "text"
	Text string `json:"text"`
}

func (ToolTextContent) toolContentTag() {}

type ToolFileContent struct {
	Type string  `json:"type"` // must be "file"
	URI  string  `json:"uri"`
	Mime string  `json:"mime"`
	Name *string `json:"name,omitempty"`
}

func (ToolFileContent) toolContentTag() {}

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

type SystemPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Cache    *CacheHint     `json:"cache,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func NewSystemPart(text string) SystemPart {
	return SystemPart{Type: "text", Text: text}
}

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

type TextPart struct {
	Type             string           `json:"type"` // must be "text"
	Text             string           `json:"text"`
	Cache            *CacheHint       `json:"cache,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (TextPart) contentPartTag() {}

// --- Media Part ---

type MediaPart struct {
	Type      string         `json:"type"` // must be "media"
	MediaType string         `json:"mediaType"`
	Data      any            `json:"data"`
	Filename  *string        `json:"filename,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (MediaPart) contentPartTag() {}

// --- Reasoning Part ---

type ReasoningPart struct {
	Type             string           `json:"type"` // must be "reasoning"
	Text             string           `json:"text"`
	Encrypted        *string          `json:"encrypted,omitempty"`
	Metadata         map[string]any   `json:"metadata,omitempty"`
	ProviderMetadata ProviderMetadata `json:"providerMetadata,omitempty"`
}

func (ReasoningPart) contentPartTag() {}

// --- Tool Call Part ---

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

type ToolCallPartInput struct {
	ID               string
	Name             string
	Input            any
	ProviderExecuted *bool
	Metadata         map[string]any
	ProviderMetadata ProviderMetadata
}

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

type ToolResultValue struct {
	Type  string `json:"type"` // "json", "text", "error", "content"
	Value any    `json:"value"`
}

func IsToolResultValue(v any) bool {
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

func NewToolResultValue(value any, typ ...string) ToolResultValue {
	if rv, ok := value.(ToolResultValue); ok {
		return rv
	}
	if IsToolResultValue(value) {
		m := value.(map[string]any)
		t, _ := m["type"].(string)
		return ToolResultValue{Type: t, Value: m["value"]}
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

type ToolOutput struct {
	Structured any           `json:"structured"`
	Content    []ToolContent `json:"content"`
}

func NewToolOutput(structured any, content ...ToolContent) ToolOutput {
	return ToolOutput{Structured: structured, Content: content}
}

func ToolOutputFromResultValue(result ToolResultValue) *ToolOutput {
	switch result.Type {
	case "json":
		return &ToolOutput{Structured: result.Value}
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

type ContentPart interface {
	contentPartTag()
}

// --- Response Format ---

type ResponseFormatText struct {
	Type string `json:"type"` // "text"
}

func (ResponseFormatText) responseFormatTag() {}

type ResponseFormatJSON struct {
	Type   string     `json:"type"` // "json"
	Schema JsonSchema `json:"schema"`
}

func (ResponseFormatJSON) responseFormatTag() {}

type ResponseFormatTool struct {
	Type string         `json:"type"` // "tool"
	Tool ToolDefinition `json:"tool"`
}

func (ResponseFormatTool) responseFormatTag() {}

type ResponseFormat interface {
	responseFormatTag()
}

// --- Tool Definition ---

type ToolDefinition struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  JsonSchema     `json:"inputSchema"`
	OutputSchema *JsonSchema    `json:"outputSchema,omitempty"`
	Cache        *CacheHint     `json:"cache,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Native       map[string]any `json:"native,omitempty"`
}

type ToolDef = ToolDefinition

type ToolDefinitionInput struct {
	Name         string
	Description  string
	InputSchema  JsonSchema
	OutputSchema *JsonSchema
	Cache        *CacheHint
	Metadata     map[string]any
	Native       map[string]any
}

func NewToolDefinition(input any) ToolDefinition {
	switch v := input.(type) {
	case ToolDefinition:
		return v
	case *ToolDefinition:
		return *v
	case ToolDefinitionInput:
		return ToolDefinition{
			Name: v.Name, Description: v.Description, InputSchema: v.InputSchema,
			OutputSchema: v.OutputSchema, Cache: v.Cache, Metadata: v.Metadata, Native: v.Native,
		}
	default:
		return ToolDefinition{}
	}
}

// --- Tool Choice ---

type ToolChoice struct {
	Type string  `json:"type"` // "auto", "none", "required", "tool"
	Name *string `json:"name,omitempty"`
}

var ToolChoiceAuto = ToolChoice{Type: "auto"}
var ToolChoiceNone = ToolChoice{Type: "none"}
var ToolChoiceRequired = ToolChoice{Type: "required"}

func ToolChoiceNamed(name string) ToolChoice {
	return ToolChoice{Type: "tool", Name: &name}
}

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

type Message struct {
	ID       string         `json:"id,omitempty"`
	Role     MessageRole    `json:"role"`
	Content  []ContentPart  `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Native   map[string]any `json:"native,omitempty"`
}

func NewTextContent(value string) ContentPart {
	return TextPart{Type: "text", Text: value}
}

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

type MessageInput struct {
	ID       string
	Role     MessageRole
	Content  any // string | ContentPart | []ContentPart
	Metadata map[string]any
	Native   map[string]any
}

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

func MessageUser(content any) Message {
	return NewMessage(MessageInput{Role: RoleUser, Content: content})
}

func MessageAssistant(content any) Message {
	return NewMessage(MessageInput{Role: RoleAssistant, Content: content})
}

func MessageSystem(content any) Message {
	return NewMessage(MessageInput{Role: RoleSystem, Content: content})
}

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

func LLMRequestInput(r *LLMRequest) map[string]any {
	return map[string]any{
		"id":              r.ID,
		"model":           r.Model,
		"system":          r.System,
		"messages":        r.Messages,
		"tools":           r.Tools,
		"toolChoice":      r.ToolChoice,
		"generation":      r.Generation,
		"providerOptions": r.ProviderOptions,
		"http":            r.Http,
		"responseFormat":  r.ResponseFormat,
		"cache":           r.Cache,
		"metadata":        r.Metadata,
	}
}

func LLMRequestUpdate(r *LLMRequest, patch map[string]any) *LLMRequest {
	if len(patch) == 0 {
		return r
	}
	updated := *r
	if v, ok := patch["model"]; ok {
		if m, ok2 := v.(ModelSchema); ok2 {
			updated.Model = m
		}
	}
	if v, ok := patch["id"]; ok {
		if s, ok2 := v.(string); ok2 {
			updated.ID = s
		}
	}
	if v, ok := patch["system"]; ok {
		if s, ok2 := v.([]SystemPart); ok2 {
			updated.System = s
		}
	}
	if v, ok := patch["messages"]; ok {
		if m, ok2 := v.([]Message); ok2 {
			updated.Messages = m
		}
	}
	if v, ok := patch["tools"]; ok {
		if t, ok2 := v.([]ToolDefinition); ok2 {
			updated.Tools = t
		}
	}
	if v, ok := patch["toolChoice"]; ok {
		if tc, ok2 := v.(*ToolChoice); ok2 {
			updated.ToolChoice = tc
		}
	}
	if v, ok := patch["generation"]; ok {
		if g, ok2 := v.(*GenerationOptions); ok2 {
			updated.Generation = g
		}
	}
	if v, ok := patch["providerOptions"]; ok {
		if po, ok2 := v.(ProviderOptions); ok2 {
			updated.ProviderOptions = po
		}
	}
	if v, ok := patch["http"]; ok {
		if h, ok2 := v.(*HttpOptions); ok2 {
			updated.Http = h
		}
	}
	if v, ok := patch["responseFormat"]; ok {
		if rf, ok2 := v.(ResponseFormat); ok2 {
			updated.ResponseFormat = rf
		}
	}
	if v, ok := patch["cache"]; ok {
		if c, ok2 := v.(*CachePolicy); ok2 {
			updated.Cache = c
		}
	}
	if v, ok := patch["metadata"]; ok {
		if m, ok2 := v.(map[string]any); ok2 {
			updated.Metadata = m
		}
	}
	return &updated
}
