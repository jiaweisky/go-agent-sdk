package llm

import (
	"context"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/jsonschema"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

func TestToolDefinitionAndValidation(t *testing.T) {
	tool := Make(ToolConfig{
		Description: "echo",
		Parameters: jsonschema.Object(
			jsonschema.Required("message", jsonschema.String()),
			jsonschema.Optional("suffix", jsonschema.String()),
		),
		OutputSchema: jsonschema.Object(jsonschema.Required("message", jsonschema.String())),
		Execute: func(ctx context.Context, params any, info *ToolExecuteContext) (any, error) {
			return params, nil
		},
	})

	definitions := ToDefinitions(Tools{"echo": tool})
	if len(definitions) != 1 {
		t.Fatalf("definitions = %+v", definitions)
	}
	if definitions[0].InputSchema["additionalProperties"] != false {
		t.Fatalf("input schema = %+v", definitions[0].InputSchema)
	}
	if definitions[0].OutputSchema == nil || (*definitions[0].OutputSchema)["additionalProperties"] != false {
		t.Fatalf("output schema = %+v", definitions[0].OutputSchema)
	}

	if _, err := tool.Decode(map[string]any{"message": "hi"}); err != nil {
		t.Fatalf("decode valid: %v", err)
	}
	if _, err := tool.Decode(map[string]any{"message": "hi", "suffix": nil}); err == nil {
		t.Fatal("expected explicit null optional to be rejected")
	}
	if _, err := tool.Decode(map[string]any{}); err == nil {
		t.Fatal("expected missing required field to be rejected")
	}

	if _, err := tool.Encode(map[string]any{"message": "hi"}); err != nil {
		t.Fatalf("encode valid: %v", err)
	}
	if _, err := tool.Encode(map[string]any{"message": 1}); err == nil {
		t.Fatal("expected invalid output to be rejected")
	}
}

func TestDispatchLegacyToolResultValue(t *testing.T) {
	tool := Make(ToolConfig{
		Description: "legacy",
		Execute: func(ctx context.Context, params any, info *ToolExecuteContext) (any, error) {
			return schema.ToolResultValue{Type: "text", Value: "hello"}, nil
		},
	})
	result := Dispatch(context.Background(), Tools{"legacy": tool}, schema.ToolCallPart{ID: "c1", Name: "legacy"})
	if result.Result.Type != "text" || result.Result.Value != "hello" {
		t.Fatalf("result was re-wrapped: %+v", result.Result)
	}
	if result.Output == nil || len(result.Output.Content) != 1 {
		t.Fatalf("output = %+v", result.Output)
	}
	if tc, ok := result.Output.Content[0].(schema.ToolTextContent); !ok || tc.Text != "hello" {
		t.Fatalf("output content = %+v", result.Output.Content)
	}
}
