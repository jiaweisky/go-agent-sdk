package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

type (
	LLMRequest                    = schema.LLMRequest
	LLMResponse                   = schema.LLMResponse
	LLMEvent                      = schema.LLMEvent
	LLMError                      = schema.LLMError
	LLMErrorReason                = schema.LLMErrorReason
	LLMErrorReasonKind            = schema.LLMErrorReasonKind
	PreparedRequest               = schema.PreparedRequest
	Message                       = schema.Message
	Usage                         = schema.Usage
	GenerationOptions             = schema.GenerationOptions
	ProtocolID                    = schema.ProtocolID
	RouteID                       = schema.RouteID
	ModelID                       = schema.ModelID
	ProviderID                    = schema.ProviderID
	ResponseID                    = schema.ResponseID
	ContentBlockID                = schema.ContentBlockID
	ToolCallID                    = schema.ToolCallID
	FinishReason                  = schema.FinishReason
	MessageRole                   = schema.MessageRole
	ReasoningEffort               = schema.ReasoningEffort
	TextVerbosity                 = schema.TextVerbosity
	JsonSchema                    = schema.JsonSchema
	ProviderMetadata              = schema.ProviderMetadata
	ProviderFailureClassification = schema.ProviderFailureClassification
	AuthenticationKind            = schema.AuthenticationKind
	HttpContext                   = schema.HttpContext
	HttpRequestDetails            = schema.HttpRequestDetails
	HttpResponseDetails           = schema.HttpResponseDetails
	HttpRateLimitDetails          = schema.HttpRateLimitDetails
	ToolFailure                   = schema.ToolFailure
	ToolDefinition                = schema.ToolDefinition
	ToolChoice                    = schema.ToolChoice
	Model                         = route.Model
	ModelSchema                   = schema.ModelSchema
	ToolContent                   = schema.ToolContent
	ToolTextContent               = schema.ToolTextContent
	ToolFileContent               = schema.ToolFileContent
	TextPart                      = schema.TextPart
	ReasoningPart                 = schema.ReasoningPart
	ToolResultValue               = schema.ToolResultValue
	ToolOutput                    = schema.ToolOutput
	SystemPart                    = schema.SystemPart
	MediaPart                     = schema.MediaPart
	ContentPart                   = schema.ContentPart
	ResponseFormat                = schema.ResponseFormat
	CacheHint                     = schema.CacheHint
	CachePolicy                   = schema.CachePolicy
	CachePolicyMessages           = schema.CachePolicyMessages
	ModelInput                    = schema.ModelInput
	MessageInput                  = schema.MessageInput
	ToolResultPartInput           = schema.ToolResultPartInput
	ToolCallPartInput             = schema.ToolCallPartInput
	ToolDefinitionInput           = schema.ToolDefinitionInput
)

const (
	RoleSystem    = schema.RoleSystem
	RoleUser      = schema.RoleUser
	RoleAssistant = schema.RoleAssistant
	RoleTool      = schema.RoleTool

	ReasonStop          = schema.ReasonStop
	ReasonLength        = schema.ReasonLength
	ReasonToolCalls     = schema.ReasonToolCalls
	ReasonContentFilter = schema.ReasonContentFilter
	ReasonError         = schema.ReasonError
	ReasonUnknown       = schema.ReasonUnknown

	ReasoningEffortNone    = schema.ReasoningEffortNone
	ReasoningEffortMinimal = schema.ReasoningEffortMinimal
	ReasoningEffortLow     = schema.ReasoningEffortLow
	ReasoningEffortMedium  = schema.ReasoningEffortMedium
	ReasoningEffortHigh    = schema.ReasoningEffortHigh
	ReasoningEffortXHigh   = schema.ReasoningEffortXHigh
	ReasoningEffortMax     = schema.ReasoningEffortMax

	TextVerbosityLow    = schema.TextVerbosityLow
	TextVerbosityMedium = schema.TextVerbosityMedium
	TextVerbosityHigh   = schema.TextVerbosityHigh

	ClassificationContextOverflow = schema.ClassificationContextOverflow

	EventTextDelta  = schema.EventTextDelta
	EventToolCall   = schema.EventToolCall
	EventToolResult = schema.EventToolResult

	AuthKindMissing                 = schema.AuthKindMissing
	AuthKindInvalid                 = schema.AuthKindInvalid
	AuthKindExpired                 = schema.AuthKindExpired
	AuthKindInsufficientPermissions = schema.AuthKindInsufficientPermissions
	AuthKindUnknown                 = schema.AuthKindUnknown
)

const (
	ReasonInvalidRequest        = schema.ReasonInvalidRequest
	ReasonNoRoute               = schema.ReasonNoRoute
	ReasonAuthentication        = schema.ReasonAuthentication
	ReasonRateLimit             = schema.ReasonRateLimit
	ReasonQuotaExceeded         = schema.ReasonQuotaExceeded
	ReasonContentPolicy         = schema.ReasonContentPolicy
	ReasonProviderInternal      = schema.ReasonProviderInternal
	ReasonTransport             = schema.ReasonTransport
	ReasonInvalidProviderOutput = schema.ReasonInvalidProviderOutput
	ReasonUnknownProvider       = schema.ReasonUnknownProvider
)

// ToolChoiceMode is a tool-choice value that names a mode rather than a tool.
type ToolChoiceMode string

const (
	ToolChoiceModeAuto     ToolChoiceMode = "auto"
	ToolChoiceModeNone     ToolChoiceMode = "none"
	ToolChoiceModeRequired ToolChoiceMode = "required"
)

// RequestInput is the ergonomic form of a request. System, Prompt and
// ToolChoice accept strings, parts, or the schema types; NewRequest normalizes
// them.
type RequestInput struct {
	ID              string
	Model           schema.ModelSchema
	System          any
	Prompt          any
	Messages        []schema.Message
	Tools           []schema.ToolDefinition
	ToolChoice      any
	Generation      *schema.GenerationOptions
	ProviderOptions schema.ProviderOptions
	Http            *schema.HttpOptions
	ResponseFormat  schema.ResponseFormat
	Cache           *schema.CachePolicy
	Metadata        map[string]any
}

// RequestInputFrom converts a request back to its input form.
func RequestInputFrom(r *schema.LLMRequest) RequestInput {
	return RequestInput{
		ID:              r.ID,
		Model:           r.Model,
		System:          r.System,
		Messages:        r.Messages,
		Tools:           r.Tools,
		ToolChoice:      r.ToolChoice,
		Generation:      r.Generation,
		ProviderOptions: r.ProviderOptions,
		Http:            r.Http,
		ResponseFormat:  r.ResponseFormat,
		Cache:           r.Cache,
		Metadata:        r.Metadata,
	}
}

// NewRequest normalizes input into a request, converting its messages, tools
// and tool choice.
func NewRequest(input RequestInput) *schema.LLMRequest {
	system := schema.SystemParts(input.System)
	messages := make([]schema.Message, 0, len(input.Messages))
	for _, m := range input.Messages {
		messages = append(messages, schema.NewMessage(m))
	}
	if input.Prompt != nil {
		messages = append(messages, schema.MessageUser(input.Prompt))
	}
	tools := make([]schema.ToolDefinition, 0, len(input.Tools))
	for _, t := range input.Tools {
		tools = append(tools, schema.NewToolDefinition(t))
	}
	var toolChoice *schema.ToolChoice
	if input.ToolChoice != nil {
		tc := schema.NewToolChoice(input.ToolChoice)
		toolChoice = &tc
	}
	var generation *schema.GenerationOptions
	if input.Generation != nil {
		generation = input.Generation
	}
	return &schema.LLMRequest{
		ID:              input.ID,
		Model:           input.Model,
		System:          system,
		Messages:        messages,
		Tools:           tools,
		ToolChoice:      toolChoice,
		Generation:      generation,
		ProviderOptions: input.ProviderOptions,
		Http:            input.Http,
		ResponseFormat:  input.ResponseFormat,
		Cache:           input.Cache,
		Metadata:        input.Metadata,
	}
}

// UpdateRequest applies the set fields of patch and returns a new request.
func UpdateRequest(r *schema.LLMRequest, patch RequestInput) *schema.LLMRequest {
	base := RequestInputFrom(r)
	if patch.System != nil {
		base.System = patch.System
	}
	if patch.Prompt != nil {
		base.Prompt = patch.Prompt
	}
	if patch.Messages != nil {
		base.Messages = patch.Messages
	}
	if patch.Tools != nil {
		base.Tools = patch.Tools
	}
	if patch.ToolChoice != nil {
		base.ToolChoice = patch.ToolChoice
	}
	if patch.ProviderOptions != nil {
		base.ProviderOptions = patch.ProviderOptions
	}
	if patch.Cache != nil {
		base.Cache = patch.Cache
	}
	if patch.Metadata != nil {
		base.Metadata = patch.Metadata
	}
	if patch.ID != "" {
		base.ID = patch.ID
	}
	base.Model = patch.Model
	base.Generation = patch.Generation
	base.Http = patch.Http
	base.ResponseFormat = patch.ResponseFormat
	return NewRequest(base)
}

const generateObjectToolName = "generate_object"
const generateObjectToolDescription = "Return the structured result by calling this tool."

// GenerateObjectResponse is a structured generation: the object the model
// returned and the response it came from.
type GenerateObjectResponse struct {
	Object   any
	Response *schema.LLMResponse
}

// Events returns the events of the underlying response.
func (r *GenerateObjectResponse) Events() []schema.LLMEvent {
	return r.Response.Events
}

// Usage returns the token usage of the underlying response.
func (r *GenerateObjectResponse) Usage() *schema.Usage {
	return r.Response.Usage
}

// GenerateObjectOptions describes a structured generation: the request, and the
// schema the returned object must match.
type GenerateObjectOptions struct {
	Request    RequestInput
	Schema     schema.JsonSchema
	JsonSchema schema.JsonSchema
}

func compileSchema(raw schema.JsonSchema) *jsonschema.Schema {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", document); err != nil {
		return nil
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return nil
	}
	return sch
}

// GenerateObject forces a single tool call and returns its validated input as
// the object.
func GenerateObject(ctx context.Context, client route.LLMClient, opts GenerateObjectOptions) (*GenerateObjectResponse, error) {
	toolInputSchema := opts.JsonSchema
	if opts.Schema != nil {
		toolInputSchema = opts.Schema
	}
	schemaInstance := compileSchema(toolInputSchema)
	toolDef := schema.ToolDefinition{
		Name:        generateObjectToolName,
		Description: generateObjectToolDescription,
		InputSchema: toolInputSchema,
	}
	base := NewRequest(opts.Request)
	tools := make([]schema.ToolDefinition, 0, len(base.Tools)+1)
	tools = append(tools, base.Tools...)
	tools = append(tools, toolDef)
	tc := schema.ToolChoiceNamed(generateObjectToolName)
	req := schema.LLMRequest{
		ID:              base.ID,
		Model:           base.Model,
		System:          base.System,
		Messages:        base.Messages,
		Tools:           tools,
		ToolChoice:      &tc,
		Generation:      base.Generation,
		ProviderOptions: base.ProviderOptions,
		Http:            base.Http,
		ResponseFormat:  base.ResponseFormat,
		Cache:           base.Cache,
		Metadata:        base.Metadata,
	}
	resp, err := client.Generate(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("generateObject: %w", err)
	}
	for _, call := range resp.Events {
		if call.Type == schema.EventToolCall && call.Name == generateObjectToolName {
			if schemaInstance != nil {
				if err := schemaInstance.Validate(call.Input); err != nil {
					return nil, fmt.Errorf("generateObject: schema validation failed: %w", err)
				}
			}
			return &GenerateObjectResponse{
				Object:   call.Input,
				Response: resp,
			}, nil
		}
	}
	return nil, fmt.Errorf("generateObject: model did not call the forced `%s` tool", generateObjectToolName)
}
