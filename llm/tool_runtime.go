package llm

import (
	"fmt"

	"github.com/user/go-agent-sdk/llm/schema"
)

type ToolSettlement struct {
	Result schema.ToolResultValue
	Output *schema.ToolOutput
}

type DispatchResult struct {
	Result schema.ToolResultValue
	Output *schema.ToolOutput
	Events []schema.LLMEvent
}

func Dispatch(tools Tools, call schema.ToolCallPart) *DispatchResult {
	tool := tools[call.Name]
	if tool == nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Unknown tool: %s", call.Name)}, nil)
	}
	if tool.execute == nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Tool has no execute handler: %s", call.Name)}, nil)
	}
	decoded, err := tool.Decode(call.Input)
	if err != nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: err.Error()}, err)
	}
	value, err := tool.Execute(nil, decoded, &ToolExecuteContext{ID: call.ID, Name: call.Name})
	if err != nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: err.Error()}, err)
	}
	encoded, err := tool.Encode(value)
	if err != nil {
		return buildResult(call, schema.ToolResultValue{Type: "error", Value: err.Error()}, err)
	}
	output := tool.Project(decoded, call.ID, encoded)
	result := schema.ToolOutputToResultValue(output)
	return buildResult(call, result, nil, &output)
}

func buildResult(call schema.ToolCallPart, result schema.ToolResultValue, cause error, output ...*schema.ToolOutput) *DispatchResult {
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
