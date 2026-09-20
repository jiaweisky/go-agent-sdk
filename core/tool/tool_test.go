package tool

import (
	"testing"

	"github.com/user/go-agent-sdk/llm/schema"
)

func TestCloseObjectSchemas(t *testing.T) {
	input := schema.JsonSchema{
		"type": "object",
		"properties": map[string]any{
			"nested": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{"type": "string"},
				},
			},
			"list": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"value": map[string]any{"type": "string"}},
				},
			},
		},
	}
	closed := closeObjectSchemas(input)

	if closed["additionalProperties"] != false {
		t.Fatalf("top-level additionalProperties = %v", closed["additionalProperties"])
	}
	nested := closed["properties"].(map[string]any)["nested"].(map[string]any)
	if nested["additionalProperties"] != false {
		t.Fatalf("nested additionalProperties = %v", nested["additionalProperties"])
	}
	items := closed["properties"].(map[string]any)["list"].(map[string]any)["items"].(map[string]any)
	if items["additionalProperties"] != false {
		t.Fatalf("items additionalProperties = %v", items["additionalProperties"])
	}

	// The original schema must not be mutated.
	if _, ok := input["additionalProperties"]; ok {
		t.Fatalf("input schema was mutated: %+v", input)
	}
	if _, ok := input["properties"].(map[string]any)["nested"].(map[string]any)["additionalProperties"]; ok {
		t.Fatalf("nested input schema was mutated")
	}
}

func TestToolDefinitionClosesObjectSchemas(t *testing.T) {
	definition := Definition(TodoWriteName, MakeTodoWrite(TodoWriteDeps{
		Todos:      &stubTodoUpdater{},
		Permission: &bashTestPermission{},
	}))
	if definition.InputSchema["additionalProperties"] != false {
		t.Fatalf("input schema additionalProperties = %v", definition.InputSchema["additionalProperties"])
	}
	if definition.OutputSchema == nil || (*definition.OutputSchema)["additionalProperties"] != false {
		t.Fatalf("output schema additionalProperties = %v", definition.OutputSchema)
	}
}

func TestValidationStaysPermissive(t *testing.T) {
	updater := &stubTodoUpdater{}
	result := settleTodoWrite(t, TodoWriteDeps{
		Todos:      updater,
		Permission: &bashTestPermission{},
	}, map[string]any{
		"todos": []any{map[string]any{"content": "x", "status": "pending", "priority": "high"}},
		"extra": "ignored",
	})
	if result.Result.Type != "text" {
		t.Fatalf("result = %+v, want text", result.Result)
	}
	if len(updater.todos) != 1 || updater.todos[0].Content != "x" {
		t.Fatalf("todos = %+v", updater.todos)
	}
}
