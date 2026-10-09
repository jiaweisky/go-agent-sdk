package llm

import (
	"context"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// ToolSettlement is a finished tool call: its result value and output.
type ToolSettlement struct {
	Result schema.ToolResultValue
	Output *schema.ToolOutput
}

// DispatchResult is what a dispatched tool call produced.
type DispatchResult struct {
	Result schema.ToolResultValue
	Output *schema.ToolOutput
	Events []schema.LLMEvent
}

// Dispatch runs one tool call: it decodes the parameters, executes the handler,
// encodes the result and builds the events. A failure becomes an error result
// rather than a Go error.
func Dispatch(ctx context.Context, tools Tools, call schema.ToolCallPart) *DispatchResult {
	tool := tools[call.Name]
	if tool == nil {
		return buildResult(
			call,
			schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Unknown tool: %s", call.Name)},
			nil,
		)
	}
	if tool.execute == nil {
		return buildResult(
			call,
			schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Tool has no execute handler: %s", call.Name)},
			nil,
		)
	}
	decoded, err := tool.Decode(call.Input)
	if err != nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: err.Error()}, err)
	}
	value, err := tool.Execute(ctx, decoded, &ToolExecuteContext{ID: call.ID, Name: call.Name})
	if err != nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: err.Error()}, err)
	}
	encoded, err := tool.Encode(value)
	if err != nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: err.Error()}, err)
	}
	if tool.legacyResult() {
		if rv, ok := schema.AsToolResultValue(encoded); ok {
			return buildResult(call, rv, nil, schema.ToolOutputFromResultValue(rv))
		}
	}
	output := tool.Project(decoded, call.ID, encoded)
	result := schema.ToolOutputToResultValue(output)
	return buildResult(call, result, nil, &output)
}

func buildResult(
	call schema.ToolCallPart,
	result schema.ToolResultValue,
	cause error,
	output ...*schema.ToolOutput,
) *DispatchResult {
	dr := &DispatchResult{Result: result}
	if len(output) > 0 {
		dr.Output = output[0]
	}
	if result.Type == "error" {
		dr.Events = []schema.LLMEvent{
			schema.NewToolError(call.ID, call.Name, fmt.Sprint(result.Value), schema.WithEventError(cause)),
			schema.NewToolResult(call.ID, call.Name, result),
		}
	} else {
		e := schema.NewToolResult(call.ID, call.Name, result)
		if dr.Output != nil {
			e.Output = dr.Output
		}
		dr.Events = []schema.LLMEvent{e}
	}
	return dr
}
