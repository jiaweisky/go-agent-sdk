package jsonschema

import (
	"reflect"
	"testing"
)

func TestModelMatchesEffectShape(t *testing.T) {
	schema := Object(
		Required("command", String(Description("Shell command string to execute"))),
		Optional("workdir", String(Description("Working directory."))),
		Optional("timeout", Integer(
			ExclusiveMinimum(0),
			Maximum(600000),
			Description("Timeout in milliseconds."),
		)),
	)

	expected := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command string to execute"},
			"workdir": map[string]any{
				"anyOf":       []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}},
				"description": "Working directory.",
			},
			"timeout": map[string]any{
				"anyOf": []any{
					map[string]any{
						"type":  "integer",
						"allOf": []any{map[string]any{"exclusiveMinimum": 0}, map[string]any{"maximum": 600000}},
					},
					map[string]any{"type": "null"},
				},
				"description": "Timeout in milliseconds.",
			},
		},
		"required":             []any{"command"},
		"additionalProperties": false,
	}
	if got := schema.Model(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("model = %#v", got)
	}
}

func TestValidationKeepsDecoderSemantics(t *testing.T) {
	schema := Object(
		Required("command", String()),
		Optional("workdir", String(Description("Working directory."))),
	)

	expected := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string"},
			"workdir": map[string]any{"type": "string", "description": "Working directory."},
		},
		"required": []any{"command"},
	}
	if got := schema.Validation(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("validation = %#v", got)
	}
}

func TestLiteralsAndNumbers(t *testing.T) {
	literal := Literal("text-page").Model()
	if !reflect.DeepEqual(literal, map[string]any{"type": "string", "enum": []any{"text-page"}}) {
		t.Fatalf("literal = %#v", literal)
	}

	number := Number().Model()
	expectedNumber := map[string]any{"anyOf": []any{
		map[string]any{"type": "number"},
		map[string]any{"type": "string", "enum": []any{"NaN"}},
		map[string]any{"type": "string", "enum": []any{"Infinity"}},
		map[string]any{"type": "string", "enum": []any{"-Infinity"}},
	}}
	if !reflect.DeepEqual(number, expectedNumber) {
		t.Fatalf("number = %#v", number)
	}
}

func TestArrayAndUnion(t *testing.T) {
	entry := Object(Required("path", String()))
	array := Array(entry).Model()
	expectedArray := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"path": map[string]any{"type": "string"}},
			"required":             []any{"path"},
			"additionalProperties": false,
		},
	}
	if !reflect.DeepEqual(array, expectedArray) {
		t.Fatalf("array = %#v", array)
	}

	union := Union(String(), Integer()).Model()
	expectedUnion := map[string]any{"anyOf": []any{
		map[string]any{"type": "string"},
		map[string]any{"type": "integer"},
	}}
	if !reflect.DeepEqual(union, expectedUnion) {
		t.Fatalf("union = %#v", union)
	}
}
