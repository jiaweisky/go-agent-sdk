package utils

import (
	llmutils "github.com/jiaweisky/go-agent-sdk/llm/utils"
)

func removeNullSchemas(value any) any {
	if arr, ok := value.([]any); ok {
		result := make([]any, len(arr))
		for i, v := range arr {
			result[i] = removeNullSchemas(v)
		}
		return result
	}
	if !llmutils.IsRecord(value) {
		return value
	}
	m := value.(map[string]any)
	fields := make(map[string]any)
	for k, v := range m {
		if k == "anyOf" {
			continue
		}
		fields[k] = removeNullSchemas(v)
	}
	anyOf, hasAnyOf := m["anyOf"].([]any)
	if !hasAnyOf {
		return fields
	}
	variants := make([]any, 0, len(anyOf))
	for _, variant := range anyOf {
		if r, ok := variant.(map[string]any); ok && r["type"] == "null" {
			continue
		}
		variants = append(variants, removeNullSchemas(variant))
	}
	if len(variants) == 1 {
		if v, ok := variants[0].(map[string]any); ok {
			for k, v2 := range v {
				fields[k] = v2
			}
			return fields
		}
	}
	fields["anyOf"] = variants
	return fields
}

func tupleItemsSchema(items []any) any {
	projected := make([]any, len(items))
	for i, item := range items {
		projected[i] = moonshotNode(item)
	}
	if len(projected) == 0 {
		return map[string]any{}
	}
	if len(projected) == 1 {
		return projected[0]
	}
	return map[string]any{"anyOf": projected}
}

func moonshotNode(schema any) any {
	if arr, ok := schema.([]any); ok {
		result := make([]any, len(arr))
		for i, v := range arr {
			result[i] = moonshotNode(v)
		}
		return result
	}
	if !llmutils.IsRecord(schema) {
		return schema
	}
	m := schema.(map[string]any)
	if ref, ok := m["$ref"].(string); ok {
		return map[string]any{"$ref": ref}
	}
	result := make(map[string]any)
	for key, value := range m {
		switch key {
		case "items":
			if arr, ok := value.([]any); ok {
				result[key] = tupleItemsSchema(arr)
			} else {
				result[key] = moonshotNode(value)
			}
		case "prefixItems":
			if _, hasItems := m["items"]; hasItems {
				continue
			}
			arr, ok := value.([]any)
			if !ok {
				arr = []any{}
			}
			result["items"] = tupleItemsSchema(arr)
		case "unevaluatedItems":
			continue
		default:
			result[key] = moonshotNode(value)
		}
	}
	return result
}

// ToolSchemaMoonshot rewrites a tool schema into the shape Moonshot requires.
func ToolSchemaMoonshot(schema map[string]any) map[string]any {
	projected := moonshotNode(schema)
	if r, ok := projected.(map[string]any); ok {
		return r
	}
	return map[string]any{}
}

// ToolSchemaGemini rewrites a tool schema into the shape Gemini requires.
func ToolSchemaGemini(schema map[string]any) map[string]any {
	r := GeminiConvert(schema)
	if r != nil {
		return r
	}
	return map[string]any{}
}

// ToolSchemaOpenAI leaves a tool schema as the OpenAI API expects it.
func ToolSchemaOpenAI(schema map[string]any) map[string]any {
	anyOf, hasAnyOf := schema["anyOf"].([]any)
	var flattened map[string]any
	if !hasAnyOf || len(anyOf) == 0 {
		flattened = make(map[string]any)
		for k, v := range schema {
			flattened[k] = v
		}
		flattened["type"] = "object"
	} else {
		variants := make([]map[string]any, 0, len(anyOf))
		for _, v := range anyOf {
			if r, ok := v.(map[string]any); ok {
				variants = append(variants, r)
			}
		}
		flattened = make(map[string]any)
		for k, v := range schema {
			if k == "anyOf" {
				continue
			}
			flattened[k] = v
		}
		flattened["type"] = "object"
		props := make(map[string]any)
		for _, variant := range variants {
			if p, ok := variant["properties"].(map[string]any); ok {
				for k, v := range p {
					if _, exists := props[k]; !exists {
						props[k] = v
					}
				}
			}
		}
		flattened["properties"] = props
		flattened["additionalProperties"] = false
	}
	normalized := removeNullSchemas(flattened)
	if r, ok := normalized.(map[string]any); ok {
		return r
	}
	return map[string]any{"type": "object"}
}

// ModelCompatibility applies the tool-schema shape a model requires, leaving the
// schema unchanged for unknown models.
func ModelCompatibility(schema map[string]any, compatibility string) map[string]any {
	switch compatibility {
	case "gemini":
		return ToolSchemaGemini(schema)
	case "moonshot":
		return ToolSchemaMoonshot(schema)
	default:
		return schema
	}
}
