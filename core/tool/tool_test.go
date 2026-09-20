package tool

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/user/go-agent-sdk/jsonschema"
)

// TestAllToolSchemasCompile guards against a schema that cannot be compiled:
// Make panics in that case, so this test makes the failure obvious at build
// time instead of at tool registration.
func TestAllToolSchemasCompile(t *testing.T) {
	cases := map[string]*jsonschema.Schema{
		"apply_patch.in": ApplyPatchInputSchema, "apply_patch.out": ApplyPatchOutputSchema,
		"bash.in": BashInputSchema, "bash.out": BashOutputSchema, "bash.structured": BashStructuredOutputSchema,
		"edit.in": EditInputSchema, "edit.out": EditOutputSchema,
		"glob.in": GlobInputSchema, "glob.out": GlobOutputSchema,
		"grep.in": GrepInputSchema, "grep.out": GrepOutputSchema,
		"question.in": QuestionInputSchema, "question.out": QuestionOutputSchema,
		"read.in": ReadInputSchema, "read.out": ReadOutputSchema,
		"skill.in": SkillInputSchema, "skill.out": SkillOutputSchema,
		"todowrite.in": TodoWriteInputSchema, "todowrite.out": TodoWriteOutputSchema,
		"webfetch.in": WebFetchInputSchema, "webfetch.out": WebFetchOutputSchema,
		"websearch.in": WebSearchInputSchema, "websearch.out": WebSearchOutputSchema,
		"write.in": WriteInputSchema, "write.out": WriteOutputSchema,
	}
	for name, schema := range cases {
		if schema == nil {
			t.Fatalf("%s: nil schema", name)
		}
		if _, err := json.Marshal(schema.Model()); err != nil {
			t.Errorf("%s: model marshal: %v", name, err)
		}
		if compiled := compileSchema(schema.Model()); compiled == nil {
			t.Errorf("%s: model schema failed to compile", name)
		}
		if compiled := compileSchema(schema.Validation()); compiled == nil {
			t.Errorf("%s: validation schema failed to compile", name)
		}
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

func TestBashInputModelShape(t *testing.T) {
	model := BashInputSchema.Model()
	properties := model["properties"].(map[string]any)

	if got := properties["command"]; !reflect.DeepEqual(got, map[string]any{
		"type": "string", "description": "Shell command string to execute",
	}) {
		t.Fatalf("command = %+v", got)
	}

	// Optional fields are bare (no null advertisement); they are simply not required.
	workdir := properties["workdir"].(map[string]any)
	if workdir["type"] != "string" {
		t.Fatalf("workdir = %+v", workdir)
	}
	if _, ok := workdir["anyOf"]; ok {
		t.Fatalf("workdir should not advertise null: %+v", workdir)
	}

	timeout := properties["timeout"].(map[string]any)
	if timeout["type"] != "integer" {
		t.Fatalf("timeout type = %v", timeout["type"])
	}
	allOf, ok := timeout["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("timeout allOf = %+v", timeout["allOf"])
	}
	if !reflect.DeepEqual(allOf[0], map[string]any{"exclusiveMinimum": 0}) {
		t.Fatalf("timeout allOf[0] = %+v", allOf[0])
	}

	required := model["required"].([]any)
	if !reflect.DeepEqual(required, []any{"command"}) {
		t.Fatalf("required = %+v", required)
	}
}

func TestValidationRejectsNullOptional(t *testing.T) {
	// Effect's decoder rejects null for optional fields even though its JSON
	// Schema advertises `anyOf: [T, null]`; the validation schema must keep
	// rejecting null while accepting an absent field.
	compiled := compileSchema(map[string]any{
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
