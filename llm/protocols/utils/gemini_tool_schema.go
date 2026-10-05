package utils

import (
	"fmt"

	llmutils "github.com/jiaweisky/go-agent-sdk/llm/utils"
)

var schemaIntentKeys = []string{
	"type", "properties", "items", "prefixItems", "enum", "const",
	"$ref", "additionalProperties", "patternProperties", "required",
	"not", "if", "then", "else",
}

func hasCombiner(schema any) bool {
	m, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	_, hasAnyOf := m["anyOf"].([]any)
	_, hasOneOf := m["oneOf"].([]any)
	_, hasAllOf := m["allOf"].([]any)
	return hasAnyOf || hasOneOf || hasAllOf
}

func hasSchemaIntent(schema any) bool {
	if !llmutils.IsRecord(schema) {
		return false
	}
	if hasCombiner(schema) {
		return true
	}
	m := schema.(map[string]any)
	for _, key := range schemaIntentKeys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func sanitizeNode(schema any) any {
	if !llmutils.IsRecord(schema) {
		arr, ok := schema.([]any)
		if !ok {
			return schema
		}
		result := make([]any, len(arr))
		for i, v := range arr {
			result[i] = sanitizeNode(v)
		}
		return result
	}
	m := schema.(map[string]any)
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = sanitizeEntry(k, v)
	}
	if enum, ok := result["enum"].([]any); ok && len(enum) > 0 {
		if typ, ok := result["type"].(string); ok && (typ == "integer" || typ == "number") {
			result["type"] = "string"
		}
	}
	typ, _ := result["type"].(string)
	if typ == "object" {
		filterRequired(result)
	}
	if typ == "array" && !hasCombiner(result) {
		defaultArrayItems(result)
	}
	if typ != "" && typ != "object" && !hasCombiner(result) {
		delete(result, "properties")
		delete(result, "required")
	}
	return result
}

// sanitizeEntry sanitizes one key of a schema, stringifying enum values because
// Gemini wants strings there.
func sanitizeEntry(key string, value any) any {
	arr, ok := value.([]any)
	if key != "enum" || !ok {
		return sanitizeNode(value)
	}
	strs := make([]any, len(arr))
	for i, entry := range arr {
		strs[i] = fmt.Sprint(entry)
	}
	return strs
}

// filterRequired drops required names that have no property.
func filterRequired(result map[string]any) {
	props, ok := result["properties"].(map[string]any)
	if !ok {
		return
	}
	required, ok := result["required"].([]any)
	if !ok {
		return
	}
	filtered := make([]any, 0, len(required))
	for _, name := range required {
		name, ok := name.(string)
		if !ok {
			continue
		}
		if _, exists := props[name]; exists {
			filtered = append(filtered, name)
		}
	}
	result["required"] = filtered
}

// defaultArrayItems gives a schema-less array a string item, as Gemini requires.
func defaultArrayItems(result map[string]any) {
	if _, ok := result["items"]; !ok {
		result["items"] = map[string]any{}
	}
	items, ok := result["items"].(map[string]any)
	if !ok || hasSchemaIntent(items) {
		return
	}
	items["type"] = "string"
	result["items"] = items
}

func emptyObjectSchema(schema map[string]any) bool {
	if schema["type"] != "object" {
		return false
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		_, hasAdditional := schema["additionalProperties"]
		return !hasAdditional
	}
	return false
}

func projectNode(schema any) map[string]any {
	m, ok := schema.(map[string]any)
	if !ok || emptyObjectSchema(m) {
		return nil
	}
	result := make(map[string]any)
	for _, key := range []string{"description", "required", "format", "minLength"} {
		if value, ok := m[key]; ok {
			result[key] = value
		}
	}
	projectType(m, result)
	projectEnum(m, result)
	projectProperties(m, result)
	projectItems(m, result)
	projectCombiners(m, result)
	if len(result) == 0 {
		return nil
	}
	return result
}

// projectType copies the JSON Schema type, flattening a ["string","null"] union
// into one type plus nullable.
func projectType(m, result map[string]any) {
	rawType, ok := m["type"]
	if !ok {
		return
	}
	arr, isArray := rawType.([]any)
	if !isArray {
		result["type"] = rawType
		return
	}
	nonNull := make([]any, 0, len(arr))
	hasNull := false
	for _, t := range arr {
		if t == "null" {
			hasNull = true
			continue
		}
		nonNull = append(nonNull, t)
	}
	if len(nonNull) > 0 {
		result["type"] = nonNull[0]
	}
	if hasNull {
		result["nullable"] = true
	}
}

// projectEnum renders a const as a single-value enum, and copies enum through.
func projectEnum(m, result map[string]any) {
	if constVal, ok := m["const"]; ok && constVal != nil {
		result["enum"] = []any{constVal}
		return
	}
	if enum, ok := m["enum"]; ok {
		result["enum"] = enum
	}
}

// projectProperties projects every property, dropping the ones that project to
// nothing.
func projectProperties(m, result map[string]any) {
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return
	}
	projected := make(map[string]any, len(props))
	for key, value := range props {
		if p := projectNode(value); p != nil {
			projected[key] = p
		}
	}
	if len(projected) > 0 {
		result["properties"] = projected
	}
}

// projectItems projects a single item schema, or a tuple of them.
func projectItems(m, result map[string]any) {
	items, ok := m["items"]
	if !ok {
		return
	}
	arr, isArray := items.([]any)
	if !isArray {
		if p := projectNode(items); p != nil {
			result["items"] = p
		}
		return
	}
	projected := make([]any, 0, len(arr))
	for _, item := range arr {
		if p := projectNode(item); p != nil {
			projected = append(projected, p)
		}
	}
	if len(projected) > 0 {
		result["items"] = projected
	}
}

// projectCombiners projects allOf, anyOf and oneOf.
func projectCombiners(m, result map[string]any) {
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		arr, ok := m[key].([]any)
		if !ok {
			continue
		}
		projected := make([]any, 0, len(arr))
		for _, item := range arr {
			if p := projectNode(item); p != nil {
				projected = append(projected, p)
			}
		}
		if len(projected) > 0 {
			result[key] = projected
		}
	}
}

// GeminiConvert rewrites a JSON schema into the shape Gemini accepts, or nil when it
// carries nothing Gemini can use.
func GeminiConvert(schema any) map[string]any {
	return projectNode(sanitizeNode(schema))
}
