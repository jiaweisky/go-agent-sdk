package llm

import (
	"context"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
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
	paramsSchema         schema.JsonSchema
	outputSchema         schema.JsonSchema
	paramsSchemaCompiled *jsonschema.Schema
	outputSchemaCompiled *jsonschema.Schema
	execute              func(ctx context.Context, params any, info *ToolExecuteContext) (any, error)
	toModelOutput        func(input ToModelOutputInput) []schema.ToolContent
	toStructuredOutput   func(output any) any
}

func (t *Tool) Description() string             { return t.description }
func (t *Tool) InputSchema() schema.JsonSchema  { return t.paramsSchema }
func (t *Tool) OutputSchema() schema.JsonSchema { return t.outputSchema }

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

type ToolConfig struct {
	Description        string
	Parameters         schema.JsonSchema
	OutputSchema       schema.JsonSchema
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
		t.paramsSchemaCompiled = compileSchema(t.paramsSchema)
	}
	if t.outputSchema != nil {
		t.outputSchemaCompiled = compileSchema(t.outputSchema)
	}
	return t
}

type Tools map[string]*Tool

func ToDefinitions(tools Tools) []schema.ToolDefinition {
	result := make([]schema.ToolDefinition, 0, len(tools))
	for name, tool := range tools {
		def := schema.ToolDefinition{
			Name:        name,
			Description: tool.description,
			InputSchema: tool.paramsSchema,
		}
		if tool.outputSchema != nil {
			out := tool.outputSchema
			def.OutputSchema = &out
		}
		result = append(result, def)
	}
	return result
}

type ToolDef = schema.ToolDef
type ToolCallPart = schema.ToolCallPart
type ToolResultPart = schema.ToolResultPart
