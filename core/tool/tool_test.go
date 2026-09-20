package tool

import (
	"reflect"
	"testing"

	"github.com/user/go-agent-sdk/llm/schema"
)

func TestModelFacingSchema(t *testing.T) {
	input := schema.JsonSchema{
		"type": "object",
		"properties": map[string]any{
			"required": map[string]any{"type": "string"},
			"optional": map[string]any{"type": "string", "description": "d"},
			"nested": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{"type": "string"},
				},
				"required": []any{"value"},
			},
			"list": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{"value": map[string]any{"type": "string"}},
					"required":   []any{"value"},
				},
			},
		},
		"required": []any{"required", "nested", "list"},
	}
	transformed := modelFacingSchema(input)

	if transformed["additionalProperties"] != false {
		t.Fatalf("top-level additionalProperties = %v", transformed["additionalProperties"])
	}
	properties := transformed["properties"].(map[string]any)

	// Required property stays a bare schema.
	if got := properties["required"]; !reflect.DeepEqual(got, map[string]any{"type": "string"}) {
		t.Fatalf("required property = %+v", got)
	}

	// Optional property is wrapped in anyOf [schema, null].
	optional := properties["optional"].(map[string]any)
	anyOf, ok := optional["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("optional property = %+v", optional)
	}
	if !reflect.DeepEqual(anyOf[0], map[string]any{"type": "string", "description": "d"}) {
		t.Fatalf("optional anyOf[0] = %+v", anyOf[0])
	}
	if !reflect.DeepEqual(anyOf[1], map[string]any{"type": "null"}) {
		t.Fatalf("optional anyOf[1] = %+v", anyOf[1])
	}

	nested := properties["nested"].(map[string]any)
	if nested["additionalProperties"] != false {
		t.Fatalf("nested additionalProperties = %v", nested["additionalProperties"])
	}
	items := properties["list"].(map[string]any)["items"].(map[string]any)
	if items["additionalProperties"] != false {
		t.Fatalf("items additionalProperties = %v", items["additionalProperties"])
	}

	// The original schema must not be mutated.
	if _, ok := input["additionalProperties"]; ok {
		t.Fatalf("input schema was mutated: %+v", input)
	}
	originalProperties := input["properties"].(map[string]any)
	if _, ok := originalProperties["nested"].(map[string]any)["additionalProperties"]; ok {
		t.Fatalf("nested input schema was mutated")
	}
	if _, ok := originalProperties["optional"].(map[string]any)["anyOf"]; ok {
		t.Fatalf("optional input schema was mutated")
	}
}

func TestModelFacingEffectShape(t *testing.T) {
	// Schema.Literal -> typed enum.
	literal := modelFacingSchema(schema.JsonSchema{"const": "text-page"})
	if !reflect.DeepEqual(literal, schema.JsonSchema{"type": "string", "enum": []any{"text-page"}}) {
		t.Fatalf("literal = %+v", literal)
	}
	// Bare enum -> typed enum.
	enum := modelFacingSchema(schema.JsonSchema{"enum": []any{"file", "directory"}})
	if !reflect.DeepEqual(enum, schema.JsonSchema{"type": "string", "enum": []any{"file", "directory"}}) {
		t.Fatalf("enum = %+v", enum)
	}
	// Schema.Number -> NaN/Infinity anyOf.
	number := modelFacingSchema(schema.JsonSchema{"type": "number"})
	expectedNumber := schema.JsonSchema{"anyOf": []any{
		map[string]any{"type": "number"},
		map[string]any{"type": "string", "enum": []any{"NaN"}},
		map[string]any{"type": "string", "enum": []any{"Infinity"}},
		map[string]any{"type": "string", "enum": []any{"-Infinity"}},
	}}
	if !reflect.DeepEqual(number, expectedNumber) {
		t.Fatalf("number = %+v", number)
	}
	// Int checks -> allOf.
	integer := modelFacingSchema(schema.JsonSchema{"type": "integer", "exclusiveMinimum": 0, "maximum": 600000})
	expectedInteger := schema.JsonSchema{"type": "integer", "allOf": []any{
		map[string]any{"exclusiveMinimum": 0},
		map[string]any{"maximum": 600000},
	}}
	if !reflect.DeepEqual(integer, expectedInteger) {
		t.Fatalf("integer = %+v", integer)
	}
}

func TestToolDefinitionModelFacing(t *testing.T) {
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
	// The todo info object has no optional fields, so its nested struct stays bare.
	item := definition.InputSchema["properties"].(map[string]any)["todos"].(map[string]any)["items"].(map[string]any)
	if item["additionalProperties"] != false {
		t.Fatalf("todo item additionalProperties = %v", item["additionalProperties"])
	}
	if _, wrapped := item["anyOf"]; wrapped {
		t.Fatalf("todo item should not be wrapped: %+v", item)
	}
}

func TestValidationRejectsNullOptional(t *testing.T) {
	// Effect's decoder rejects null for optional fields even though its JSON
	// Schema advertises `anyOf: [T, null]`; the validation schema must keep
	// rejecting null while accepting an absent field.
	compiled := compileSchema(schema.JsonSchema{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "string"},
		},
		"required": []any{"b"},
	})
	type input struct {
		A *string `json:"a"`
		B string  `json:"b"`
	}
	if err := decodeAndValidate(map[string]any{"a": nil, "b": "x"}, &input{}, compiled); err == nil {
		t.Fatal("expected explicit null optional field to be rejected")
	}
	if err := decodeAndValidate(map[string]any{"b": "x"}, &input{}, compiled); err != nil {
		t.Fatalf("absent optional field should be accepted: %v", err)
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
