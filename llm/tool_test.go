package llm

import (
	"context"
	"testing"

	"github.com/user/go-agent-sdk/jsonschema"
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
