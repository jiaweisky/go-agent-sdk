// Package jsonschema provides a typed JSON Schema model for tool definitions.
//
// It covers the subset of JSON Schema the core tools need. A single Schema
// yields two projections from one source:
//
//   - Model renders the JSON Schema sent to models. Object schemas are closed
//     with `additionalProperties: false`, optional fields are simply omitted
//     from `required`, literals become typed enums, and numeric checks live
//     inside `allOf`.
//
//   - Validation renders the schema used to validate decoded input/output.
//     Unknown object properties are allowed (and stripped) and optional fields
//     are not required.
//
// The two projections differ only in object closure. Crucially, neither
// advertises a value the decoder rejects: optional fields do not accept null,
// and numbers do not accept the string encodings NaN/Infinity/-Infinity.
package jsonschema

type kind int

const (
	kindString kind = iota
	kindInteger
	kindNumber
	kindBoolean
	kindEnum
	kindArray
	kindObject
	kindUnion
	kindRaw
)

// projection selects which of the two renderings a schema produces.
type projection int

const (
	projectionModel projection = iota
	projectionValidation
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
}

// Required declares a required object property.
func Required(name string, schema *Schema) Field {
	return Field{name: name, schema: schema}
}

// Optional declares an optional object property: it may be absent, but when
// present it must match the schema (null is not accepted).
func Optional(name string, schema *Schema) Field {
	return Field{name: name, schema: schema, optional: true}
}

// Option customizes a schema.
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

// String is a string schema.
func String(options ...Option) *Schema { return newSchema(kindString, options) }

// Integer is a JSON integer schema.
func Integer(options ...Option) *Schema { return newSchema(kindInteger, options) }

// Number is a JSON number schema.
func Number(options ...Option) *Schema { return newSchema(kindNumber, options) }

// Boolean is a JSON boolean schema.
func Boolean(options ...Option) *Schema { return newSchema(kindBoolean, options) }

// Literal is a typed enum of the given values.
func Literal(values ...any) *Schema { return Enum(values) }

// Enum is a typed enum with options (for example a description).
func Enum(values []any, options ...Option) *Schema {
	s := newSchema(kindEnum, options)
	s.enum = append([]any(nil), values...)
	return s
}

// Array is a schema for an array whose items match items.
func Array(items *Schema, options ...Option) *Schema {
	s := newSchema(kindArray, options)
	s.items = items
	return s
}

// Object is an object schema with the given fields.
func Object(fields ...Field) *Schema {
	return &Schema{kind: kindObject, fields: append([]Field(nil), fields...)}
}

// Union is a schema matching any of the given variants.
func Union(variants ...*Schema) *Schema {
	return &Schema{kind: kindUnion, variants: append([]*Schema(nil), variants...)}
}

// Raw wraps an externally-authored JSON Schema unchanged.
func Raw(schema map[string]any) *Schema {
	return &Schema{kind: kindRaw, raw: schema}
}

func newSchema(k kind, options []Option) *Schema {
	s := &Schema{kind: k}
	apply(s, options)
	return s
}

func apply(s *Schema, options []Option) {
	for _, option := range options {
		option(s)
	}
}

// With returns a shallow copy of the schema with the given options applied.
func (s *Schema) With(options ...Option) *Schema {
	copied := *s
	copied.constraints = append([]constraint(nil), s.constraints...)
	copied.fields = append([]Field(nil), s.fields...)
	copied.variants = append([]*Schema(nil), s.variants...)
	copied.enum = append([]any(nil), s.enum...)
	apply(&copied, options)
	return &copied
}

// Model renders the schema sent to models.
func (s *Schema) Model() map[string]any {
	return s.render(projectionModel)
}

// Validation renders the schema used to validate decoded input/output.
func (s *Schema) Validation() map[string]any {
	return s.render(projectionValidation)
}

func (s *Schema) render(p projection) map[string]any {
	out := s.renderKind(p)
	if s.description != "" {
		out["description"] = s.description
	}
	if s.hasNumericConstraints() {
		out["allOf"] = renderConstraints(s.constraints)
	}
	return out
}

func (s *Schema) renderKind(p projection) map[string]any {
	switch s.kind {
	case kindString:
		return map[string]any{"type": "string"}
	case kindInteger:
		return map[string]any{"type": "integer"}
	case kindNumber:
		return map[string]any{"type": "number"}
	case kindBoolean:
		return map[string]any{"type": "boolean"}
	case kindEnum:
		return renderEnum(s.enum)
	case kindArray:
		return renderArray(s.items, p)
	case kindObject:
		return renderObject(s.fields, p)
	case kindUnion:
		return renderUnion(s.variants, p)
	case kindRaw:
		return renderRaw(s.raw)
	default:
		return map[string]any{}
	}
}

func renderEnum(values []any) map[string]any {
	out := map[string]any{"enum": append([]any(nil), values...)}
	if len(values) > 0 {
		if name, ok := enumType(values[0]); ok {
			out["type"] = name
		}
	}
	return out
}

func renderArray(items *Schema, p projection) map[string]any {
	out := map[string]any{"type": "array"}
	if items != nil {
		out["items"] = items.render(p)
	}
	return out
}

func renderObject(fields []Field, p projection) map[string]any {
	out := map[string]any{"type": "object"}
	properties := make(map[string]any, len(fields))
	required := make([]any, 0, len(fields))
	for _, field := range fields {
		properties[field.name] = field.schema.render(p)
		if !field.optional {
			required = append(required, field.name)
		}
	}
	out["properties"] = properties
	if len(required) > 0 {
		out["required"] = required
	}
	if p == projectionModel {
		out["additionalProperties"] = false
	}
	return out
}

func renderUnion(variants []*Schema, p projection) map[string]any {
	rendered := make([]any, 0, len(variants))
	for _, variant := range variants {
		rendered = append(rendered, variant.render(p))
	}
	return map[string]any{"anyOf": rendered}
}

func renderRaw(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		out[key] = value
	}
	return out
}

func (s *Schema) hasNumericConstraints() bool {
	return (s.kind == kindInteger || s.kind == kindNumber) && len(s.constraints) > 0
}

func renderConstraints(constraints []constraint) []any {
	allOf := make([]any, 0, len(constraints))
	for _, c := range constraints {
		allOf = append(allOf, map[string]any{c.key: c.value})
	}
	return allOf
}

// enumType reports the JSON type of an enum value. JSON Schema requires enum
// members to share a type, so the first value determines the advertised type.
// The boolean is false for values whose JSON type is unknown (objects, arrays);
// the caller then omits `type` rather than misreporting it.
func enumType(value any) (string, bool) {
	switch value.(type) {
	case string:
		return "string", true
	case bool:
		return "boolean", true
	case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number", true
	case nil:
		return "null", true
	default:
		return "", false
	}
}
