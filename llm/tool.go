package llm

import (
	"context"
	"fmt"

	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm/schema"
)

type ToolExecuteContext struct {
	ID   string
	Name string
}

type ToModelOutputInput struct {
	CallID     string
	Parameters any
	Output     any
}

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

func (t *Tool) Description() string { return t.description }

func (t *Tool) InputSchema() schema.JsonSchema {
	if t.paramsSchema == nil {
		return nil
	}
	return schema.JsonSchema(t.paramsSchema.Model())
}

func (t *Tool) OutputSchema() schema.JsonSchema {
	if t.outputSchema == nil {
		return nil
	}
	return schema.JsonSchema(t.outputSchema.Model())
}

func (t *Tool) Decode(input any) (any, error) {
	if t.paramsSchemaCompiled == nil {
		return input, nil
	}
	if err := t.paramsSchemaCompiled.Validate(input); err != nil {
		return nil, fmt.Errorf("Invalid tool input: %w", err)
	}
	return input, nil
}

func (t *Tool) Encode(value any) (any, error) {
	if t.outputSchemaCompiled == nil {
		return value, nil
	}
	if err := t.outputSchemaCompiled.Validate(value); err != nil {
		return nil, fmt.Errorf("Tool returned an invalid value for its success schema: %w", err)
	}
	return value, nil
}

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

func (t *Tool) ToModelOutput(callID string, params any, output any) []schema.ToolContent {
	if t.toModelOutput == nil {
		return nil
	}
	return t.toModelOutput(ToModelOutputInput{CallID: callID, Parameters: params, Output: output})
}

func (t *Tool) ToStructuredOutput(output any) any {
	if t.toStructuredOutput == nil {
		return output
	}
	return t.toStructuredOutput(output)
}

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

// ToolConfig mirrors the typed Tool.make mode in packages/llm/src/tool.ts: the
// parameters and success schemas drive both the model-facing definition
// (Model) and input/output validation (Validation).
type ToolConfig struct {
	Description        string
	Parameters         *jsonschema.Schema
	OutputSchema       *jsonschema.Schema
	Execute            func(ctx context.Context, params any, info *ToolExecuteContext) (any, error)
	ToModelOutput      func(input ToModelOutputInput) []schema.ToolContent
	ToStructuredOutput func(output any) any
}

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
	compiled := compileSchema(raw)
	if compiled == nil {
		panic("llm: tool schema failed to compile")
	}
	return compiled
}

type Tools map[string]*Tool

func ToDefinitions(tools Tools) []schema.ToolDefinition {
	result := make([]schema.ToolDefinition, 0, len(tools))
	for name, tool := range tools {
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

type ToolDef = schema.ToolDef
type ToolCallPart = schema.ToolCallPart
type ToolResultPart = schema.ToolResultPart
