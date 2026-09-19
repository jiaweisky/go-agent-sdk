package utils

import (
	"fmt"

	llmutils "github.com/user/go-agent-sdk/llm/utils"
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
		if arr, ok := schema.([]any); ok {
			result := make([]any, len(arr))
			for i, v := range arr {
				result[i] = sanitizeNode(v)
			}
			return result
		}
		return schema
	}
	m := schema.(map[string]any)
	result := make(map[string]any, len(m))
	for k, v := range m {
		if k == "enum" {
			if arr, ok := v.([]any); ok {
				strs := make([]any, len(arr))
				for i, e := range arr {
					strs[i] = fmt.Sprint(e)
				}
				result[k] = strs
				continue
			}
		}
		result[k] = sanitizeNode(v)
	}
	if enum, hasEnum := result["enum"]; hasEnum {
		if arr, ok := enum.([]any); ok && len(arr) > 0 {
			if typ, ok := result["type"].(string); ok && (typ == "integer" || typ == "number") {
				result["type"] = "string"
			}
		}
	}
	if typ, ok := result["type"].(string); ok && typ == "object" {
		if props, ok := result["properties"].(map[string]any); ok {
			if req, ok := result["required"].([]any); ok {
				filtered := make([]any, 0, len(req))
				for _, f := range req {
					if name, ok := f.(string); ok {
						if _, exists := props[name]; exists {
							filtered = append(filtered, name)
						}
					}
				}
				result["required"] = filtered
			}
		}
	}
	if typ, ok := result["type"].(string); ok && typ == "array" && !hasCombiner(result) {
		if _, ok := result["items"]; !ok {
			result["items"] = map[string]any{}
		}
		if items, ok := result["items"].(map[string]any); ok && !hasSchemaIntent(items) {
			items["type"] = "string"
			result["items"] = items
		}
	}
	if typ, ok := result["type"].(string); ok && typ != "object" && !hasCombiner(result) {
		delete(result, "properties")
		delete(result, "required")
	}
	return result
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
	if !ok {
		return nil
	}
	if emptyObjectSchema(m) {
		return nil
	}
	result := make(map[string]any)
	if desc, ok := m["description"]; ok {
		result["description"] = desc
	}
	if req, ok := m["required"]; ok {
		result["required"] = req
	}
	if format, ok := m["format"]; ok {
		result["format"] = format
	}
	if rawType, ok := m["type"]; ok {
		if arr, ok := rawType.([]any); ok {
			nonNull := make([]any, 0, len(arr))
			hasNull := false
			for _, t := range arr {
				if t == "null" {
					hasNull = true
				} else {
					nonNull = append(nonNull, t)
				}
			}
			if len(nonNull) > 0 {
				result["type"] = nonNull[0]
			}
			if hasNull {
				result["nullable"] = true
			}
		} else {
			result["type"] = rawType
		}
	}
	if constVal, ok := m["const"]; ok && constVal != nil {
		result["enum"] = []any{constVal}
	} else if enum, ok := m["enum"]; ok {
		result["enum"] = enum
	}
	if props, ok := m["properties"].(map[string]any); ok {
		projected := make(map[string]any, len(props))
		for k, v := range props {
			if p := projectNode(v); p != nil {
				projected[k] = p
			}
		}
		if len(projected) > 0 {
			result["properties"] = projected
		}
	}
	if items, ok := m["items"]; ok {
		if arr, ok := items.([]any); ok {
			projected := make([]any, 0, len(arr))
			for _, item := range arr {
				if p := projectNode(item); p != nil {
					projected = append(projected, p)
				}
			}
			if len(projected) > 0 {
				result["items"] = projected
			}
		} else if p := projectNode(items); p != nil {
			result["items"] = p
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if arr, ok := m[key].([]any); ok {
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
	if minLen, ok := m["minLength"]; ok {
		result["minLength"] = minLen
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func Convert(schema any) map[string]any {
	return projectNode(sanitizeNode(schema))
}
