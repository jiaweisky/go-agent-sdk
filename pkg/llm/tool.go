package llm

import (
	"context"
	"fmt"
	"sort"

	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
)

// ToolExecuteContext identifies the tool call being executed.
type ToolExecuteContext struct {
	ID   string
	Name string
}

// ToModelOutputInput is what a tool's output mapper receives: the call id, the
// decoded parameters, and the output.
type ToModelOutputInput struct {
	CallID     string
	Parameters any
	Output     any
}

// Tool is a model-facing tool: its schemas, its handler, and how its result
// reaches the model.
type Tool struct {
	description          string
	paramsSchema         *jsonschema.Schema
	outputSchema         *jsonschema.Schema
	paramsSchemaCompiled *jsonschema6.Schema
	outputSchemaCompiled *jsonschema6.Schema
	execute              func(ctx context.Context, params any, info *ToolExecuteContext) (any, error)
	toModelOutput        func(input ToModelOutputInput) []schema.ToolContent
	toStructuredOutput   func(output any) any
}

// Description returns the tool's description for the model.
func (t *Tool) Description() string { return t.description }

// InputSchema returns the model-facing schema for the tool's parameters, or nil
// when the tool takes none.
func (t *Tool) InputSchema() schema.JsonSchema {
	if t.paramsSchema == nil {
		return nil
	}
	return schema.JsonSchema(t.paramsSchema.Model())
}

// OutputSchema returns the model-facing schema for the tool's output, or nil
// when it has none.
func (t *Tool) OutputSchema() schema.JsonSchema {
	if t.outputSchema == nil {
		return nil
	}
	return schema.JsonSchema(t.outputSchema.Model())
}

// Decode validates raw parameters against the input schema.
func (t *Tool) Decode(input any) (any, error) {
	if t.paramsSchemaCompiled == nil {
		return input, nil
	}
	if err := t.paramsSchemaCompiled.Validate(input); err != nil {
		return nil, fmt.Errorf("Invalid tool input: %w", err)
	}
	return input, nil
}

// Encode validates a handler result against the success schema.
func (t *Tool) Encode(value any) (any, error) {
	if t.outputSchemaCompiled == nil {
		return value, nil
	}
	if err := t.outputSchemaCompiled.Validate(value); err != nil {
		return nil, fmt.Errorf("Tool returned an invalid value for its success schema: %w", err)
	}
	return value, nil
}

// Execute runs the tool's handler. info is optional.
func (t *Tool) Execute(ctx context.Context, params any, info ...*ToolExecuteContext) (any, error) {
	if t.execute == nil {
		return nil, nil
	}
	var inf *ToolExecuteContext
	if len(info) > 0 {
		inf = info[0]
	}
	return t.execute(ctx, params, inf)
}

// ToModelOutput renders a tool result as content for the model.
func (t *Tool) ToModelOutput(callID string, params any, output any) []schema.ToolContent {
	if t.toModelOutput == nil {
		return nil
	}
	return t.toModelOutput(ToModelOutputInput{CallID: callID, Parameters: params, Output: output})
}

// ToStructuredOutput maps a handler result to the structured output, unchanged
// when the tool has no mapper.
func (t *Tool) ToStructuredOutput(output any) any {
	if t.toStructuredOutput == nil {
		return output
	}
	return t.toStructuredOutput(output)
}

// Project builds the tool output: the structured value plus its model-facing
// content.
func (t *Tool) Project(parameters any, callID string, output any) schema.ToolOutput {
	structured := t.ToStructuredOutput(output)
	var content []schema.ToolContent
	if t.toModelOutput != nil {
		content = t.toModelOutput(ToModelOutputInput{CallID: callID, Parameters: parameters, Output: output})
	} else if s, ok := output.(string); ok {
		content = []schema.ToolContent{schema.ToolTextContent{Type: "text", Text: s}}
	}
	return schema.NewToolOutput(structured, content...)
}

// legacyResult reports whether the tool has no output mappers, so a handler
// result that already is a ToolResultValue is used as-is instead of being
// projected and re-wrapped.
func (t *Tool) legacyResult() bool {
	return t.toModelOutput == nil && t.toStructuredOutput == nil
}

// ToolConfig describes a tool. The parameters and output schemas drive both the
// model-facing definition and the input/output validation.
type ToolConfig struct {
	Description        string
	Parameters         *jsonschema.Schema
	OutputSchema       *jsonschema.Schema
	Execute            func(ctx context.Context, params any, info *ToolExecuteContext) (any, error)
	ToModelOutput      func(input ToModelOutputInput) []schema.ToolContent
	ToStructuredOutput func(output any) any
}

// Make builds a tool from config, compiling its schemas so a malformed call or
// result fails fast.
func Make(config ToolConfig) *Tool {
	t := &Tool{
		description:        config.Description,
		paramsSchema:       config.Parameters,
		outputSchema:       config.OutputSchema,
		execute:            config.Execute,
		toModelOutput:      config.ToModelOutput,
		toStructuredOutput: config.ToStructuredOutput,
	}
	if t.paramsSchema != nil {
		t.paramsSchemaCompiled = mustCompileToolSchema(schema.JsonSchema(t.paramsSchema.Validation()))
	}
	if t.outputSchema != nil {
		t.outputSchemaCompiled = mustCompileToolSchema(schema.JsonSchema(t.outputSchema.Validation()))
	}
	return t
}

// mustCompileToolSchema fails fast when a schema cannot be compiled; silently
// skipping validation would let malformed calls through.
func mustCompileToolSchema(raw schema.JsonSchema) *jsonschema6.Schema {
	compiled, err := compileSchema(raw)
	if err != nil || compiled == nil {
		panic("llm: tool schema failed to compile")
	}
	return compiled
}

// Tools is a set of tools keyed by name.
type Tools map[string]*Tool

// ToDefinitions renders the tools as model-facing definitions. Tools are
// emitted in name order so the request prefix is stable across runs.
func ToDefinitions(tools Tools) []schema.ToolDefinition {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]schema.ToolDefinition, 0, len(tools))
	for _, name := range names {
		tool := tools[name]
		def := schema.ToolDefinition{
			Name:        name,
			Description: tool.description,
		}
		if tool.paramsSchema != nil {
			def.InputSchema = tool.paramsSchema.Model()
		}
		if tool.outputSchema != nil {
			out := schema.JsonSchema(tool.outputSchema.Model())
			def.OutputSchema = &out
		}
		result = append(result, def)
	}
	return result
}

// ToolDef is the model-facing tool definition.
type ToolDef = schema.ToolDef

// ToolCallPart is one tool call the model asked for.
type ToolCallPart = schema.ToolCallPart

// ToolResultPart is one tool result sent back to the model.
type ToolResultPart = schema.ToolResultPart
