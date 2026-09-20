// Package jsonschema provides a typed JSON Schema model for tool definitions.
//
// It mirrors the subset of Effect's Schema used by core tools. A single Schema
// yields two projections from one source:
//
//   - Model renders the JSON Schema sent to models, matching Effect's
//     Schema.toJsonSchemaDocument: objects are closed with
//     `additionalProperties: false`, optional fields become
//     `anyOf: [<schema>, { "type": "null" }]`, literals become typed enums,
//     numbers also accept the string encodings NaN/Infinity/-Infinity, and
//     Int/Number checks live inside `allOf`.
//
//   - Validation renders a JSON Schema whose validation semantics match
//     Effect's runtime decoder instead: unknown object properties are allowed
//     (and stripped), and `null` is rejected for optional fields.
//
// Keeping both projections on one AST means the definition sent to models and
// the schema used to validate input can never drift apart.
package jsonschema

type kind int

const (
	kindString kind = iota
	kindInteger
	kindNumber
	kindFinite
	kindBoolean
	kindEnum
	kindArray
	kindObject
	kindUnion
	kindRaw
)

type constraint struct {
	key   string
	value any
}

// Field is a property of an object schema.
type Field struct {
	name     string
	schema   *Schema
	optional bool
	// nullable marks an Effect `Schema.optional` field, which is represented in
	// the model schema as `anyOf: [<schema>, { "type": "null" }]`. Non-nullable
	// optional fields (the custom `optionalKey` helper) stay a bare schema.
	nullable bool
}

// Required declares a required object property.
func Required(name string, schema *Schema) Field {
	return Field{name: name, schema: schema}
}

// Optional declares an optional property that also accepts null, mirroring
// Effect's `Schema.optional`.
func Optional(name string, schema *Schema) Field {
	return Field{name: name, schema: schema, optional: true, nullable: true}
}

// OptionalKey declares an optional property that must not be null, mirroring
// the custom `optionalKey` helper (the key may be absent, the value may not be
// null).
func OptionalKey(name string, schema *Schema) Field {
	return Field{name: name, schema: schema, optional: true}
}

// Option customizes a scalar schema.
type Option func(*Schema)

// Description annotates a schema with a description.
func Description(text string) Option {
	return func(s *Schema) { s.description = text }
}

// ExclusiveMinimum adds an `exclusiveMinimum` check.
func ExclusiveMinimum(value any) Option {
	return numericConstraint("exclusiveMinimum", value)
}

// Minimum adds a `minimum` check.
func Minimum(value any) Option {
	return numericConstraint("minimum", value)
}

// ExclusiveMaximum adds an `exclusiveMaximum` check.
func ExclusiveMaximum(value any) Option {
	return numericConstraint("exclusiveMaximum", value)
}

// Maximum adds a `maximum` check.
func Maximum(value any) Option {
	return numericConstraint("maximum", value)
}

// MultipleOf adds a `multipleOf` check.
func MultipleOf(value any) Option {
	return numericConstraint("multipleOf", value)
}

func numericConstraint(key string, value any) Option {
	return func(s *Schema) {
		s.constraints = append(s.constraints, constraint{key: key, value: value})
	}
}

// Schema is a JSON Schema node.
type Schema struct {
	kind        kind
	description string
	enum        []any
	items       *Schema
	fields      []Field
	variants    []*Schema
	constraints []constraint
	raw         map[string]any
}

// String is Schema.String.
func String(options ...Option) *Schema {
	s := &Schema{kind: kindString}
	apply(s, options)
	return s
}

// Integer is Schema.Int.
func Integer(options ...Option) *Schema {
	s := &Schema{kind: kindInteger}
	apply(s, options)
	return s
}

// Number is Schema.Number: a number that also accepts the string encodings of
// the non-finite values.
func Number(options ...Option) *Schema {
	s := &Schema{kind: kindNumber}
	apply(s, options)
	return s
}

// Finite is Schema.Finite: a plain JSON number.
func Finite(options ...Option) *Schema {
	s := &Schema{kind: kindFinite}
	apply(s, options)
	return s
}

// Boolean is Schema.Boolean.
func Boolean(options ...Option) *Schema {
	s := &Schema{kind: kindBoolean}
	apply(s, options)
	return s
}

// Literal is Schema.Literal / Schema.Literals: a typed enum.
func Literal(values ...any) *Schema {
	return &Schema{kind: kindEnum, enum: append([]any(nil), values...)}
}

// Enum is a typed enum with options (for example a description).
func Enum(values []any, options ...Option) *Schema {
	s := &Schema{kind: kindEnum, enum: append([]any(nil), values...)}
	apply(s, options)
	return s
}

// Array is Schema.Array.
func Array(items *Schema, options ...Option) *Schema {
	s := &Schema{kind: kindArray, items: items}
	apply(s, options)
	return s
}

// Object is Schema.Struct.
func Object(fields ...Field) *Schema {
	return &Schema{kind: kindObject, fields: append([]Field(nil), fields...)}
}

// Union is Schema.Union.
func Union(variants ...*Schema) *Schema {
	return &Schema{kind: kindUnion, variants: append([]*Schema(nil), variants...)}
}

// Raw wraps an externally-authored JSON Schema unchanged.
func Raw(schema map[string]any) *Schema {
	return &Schema{kind: kindRaw, raw: schema}
}

func apply(s *Schema, options []Option) {
	for _, option := range options {
		option(s)
	}
}

// Model renders the schema sent to models.
func (s *Schema) Model() map[string]any {
	return s.render(true)
}

// Validation renders the schema used to validate decoded input/output.
func (s *Schema) Validation() map[string]any {
	return s.render(false)
}

func (s *Schema) render(model bool) map[string]any {
	out := map[string]any{}
	switch s.kind {
	case kindString:
		out["type"] = "string"
	case kindInteger:
		out["type"] = "integer"
	case kindNumber:
		// Effect's number codec also accepts the string encodings of the
		// non-finite values.
		out["anyOf"] = []any{
			map[string]any{"type": "number"},
			map[string]any{"type": "string", "enum": []any{"NaN"}},
			map[string]any{"type": "string", "enum": []any{"Infinity"}},
			map[string]any{"type": "string", "enum": []any{"-Infinity"}},
		}
	case kindFinite:
		out["type"] = "number"
	case kindBoolean:
		out["type"] = "boolean"
	case kindEnum:
		if len(s.enum) > 0 {
			out["type"] = typeName(s.enum[0])
		}
		out["enum"] = append([]any(nil), s.enum...)
	case kindArray:
		out["type"] = "array"
		if s.items != nil {
			out["items"] = s.items.render(model)
		}
	case kindObject:
		renderObject(out, s, model)
	case kindUnion:
		variants := make([]any, 0, len(s.variants))
		for _, variant := range s.variants {
			variants = append(variants, variant.render(model))
		}
		out["anyOf"] = variants
	case kindRaw:
		for key, value := range s.raw {
			out[key] = value
		}
	}
	if s.description != "" {
		out["description"] = s.description
	}
	if (s.kind == kindInteger || s.kind == kindNumber || s.kind == kindFinite) && len(s.constraints) > 0 {
		allOf := make([]any, 0, len(s.constraints))
		for _, c := range s.constraints {
			allOf = append(allOf, map[string]any{c.key: c.value})
		}
		out["allOf"] = allOf
	}
	return out
}

func renderObject(out map[string]any, s *Schema, model bool) {
	out["type"] = "object"
	properties := make(map[string]any, len(s.fields))
	required := make([]any, 0, len(s.fields))
	for _, field := range s.fields {
		child := field.schema.render(model)
		if !field.optional {
			properties[field.name] = child
			required = append(required, field.name)
			continue
		}
		if !model || !field.nullable {
			properties[field.name] = child
			continue
		}
		// Optional fields are represented as anyOf [schema, null]; Effect
		// attaches the description to that wrapper rather than the variant.
		wrapper := map[string]any{
			"anyOf": []any{child, map[string]any{"type": "null"}},
		}
		if description, ok := child["description"]; ok {
			delete(child, "description")
			wrapper["description"] = description
		}
		properties[field.name] = wrapper
	}
	out["properties"] = properties
	if len(required) > 0 {
		out["required"] = required
	}
	if model {
		out["additionalProperties"] = false
	}
}

func typeName(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case nil:
		return "null"
	default:
		return "string"
	}
}
