package tool

import (
	"maps"
	"strings"
)

type jsonObject = map[string]any

func Normalize(value jsonObject, stripNullOpt ...bool) jsonObject {
	stripNull := len(stripNullOpt) > 0 && stripNullOpt[0]
	result := normalize(value, stripNull)
	if result == nil {
		return nil
	}
	return result.(jsonObject)
}

func normalize(value any, stripNull bool) any {
	switch v := value.(type) {
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = normalize(item, stripNull)
		}
		return result
	case map[string]any:
		return normalizeMap(v, stripNull)
	case nil:
		return nil
	default:
		return v
	}
}

func normalizeMap(value jsonObject, stripNull bool) jsonObject {
	required := make(map[string]bool)
	if req, ok := value["required"].([]any); ok {
		for _, item := range req {
			if s, ok := item.(string); ok {
				required[s] = true
			}
		}
	}

	schema := make(jsonObject, len(value))
	for k, v := range value {
		if k == "properties" {
			if props, ok := v.(jsonObject); ok {
				props2 := make(jsonObject, len(props))
				for name, prop := range props {
					props2[name] = normalize(prop, !required[name])
				}
				schema[k] = props2
			} else {
				schema[k] = v
			}
		} else {
			schema[k] = normalize(v, stripNull)
		}
	}

	if addProp, ok := schema["additionalProperties"]; ok && addProp == true {
		delete(schema, "additionalProperties")
	}

	if stripNull {
		if anyOf, ok := schema["anyOf"].([]any); ok {
			withoutNull := filterAnyOfWithoutNull(anyOf)
			if len(withoutNull) != len(anyOf) {
				schema2 := make(jsonObject, len(schema))
				maps.Copy(schema2, schema)
				schema2["anyOf"] = withoutNull
				return normalizeMap(schema2, stripNull)
			}
		}
	}

	if anyOf, ok := schema["anyOf"].([]any); ok {
		withoutNull := anyOf

		numberIdx := -1
		nonFiniteCount := 0
		for i, item := range withoutNull {
			if m, ok := item.(jsonObject); ok {
				if m["type"] == "number" {
					numberIdx = i
				}
				if enum, ok := m["enum"].([]any); ok && len(enum) > 0 {
					allNonFinite := true
					for _, e := range enum {
						if !isNonFinite(e) {
							allNonFinite = false
							break
						}
					}
					if allNonFinite {
						nonFiniteCount++
					}
				}
			}
		}

		if numberIdx >= 0 && nonFiniteCount == len(withoutNull)-1 {
			numberSchema := withoutNull[numberIdx].(jsonObject)
			merged := make(jsonObject, len(schema))
			maps.Copy(merged, schema)
			delete(merged, "anyOf")
			for k, v := range numberSchema {
				merged[k] = v
			}
			return normalizeMap(merged, stripNull)
		}

		if isEmptyStructUnion(withoutNull) {
			merged := make(jsonObject, len(schema))
			maps.Copy(merged, schema)
			delete(merged, "anyOf")
			merged["type"] = "object"
			merged["properties"] = jsonObject{}
			return normalizeMap(merged, stripNull)
		}

		if len(withoutNull) == 1 {
			if single, ok := withoutNull[0].(jsonObject); ok {
				merged := make(jsonObject, len(schema))
				maps.Copy(merged, schema)
				delete(merged, "anyOf")
				for k, v := range single {
					merged[k] = v
				}
				return normalizeMap(merged, stripNull)
			}
		}
	}

	if allOf, ok := schema["allOf"].([]any); ok {
		allRecords := true
		for _, item := range allOf {
			if _, ok := item.(jsonObject); !ok {
				allRecords = false
				break
			}
		}
		if allRecords {
			allOfObjs := make([]jsonObject, len(allOf))
			for i, item := range allOf {
				allOfObjs[i] = item.(jsonObject)
			}
			if canFlattenAllOf(allOfObjs, schema) {
				flattened := make(jsonObject, len(schema))
				maps.Copy(flattened, schema)
				delete(flattened, "allOf")
				for _, item := range allOfObjs {
					for k, v := range item {
						flattened[k] = v
					}
				}
				return normalizeMap(flattened, stripNull)
			}
		}
	}

	if typ, ok := schema["type"].(string); ok && typ == "integer" {
		if _, hasMax := schema["maximum"]; !hasMax {
			schema2 := make(jsonObject, len(schema)+2)
			maps.Copy(schema2, schema)
			schema2["minimum"] = -9007199254740991
			schema2["maximum"] = 9007199254740991
			return schema2
		}
	}

	return schema
}

func filterAnyOfWithoutNull(items []any) []any {
	result := make([]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(jsonObject); ok && m["type"] == "null" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func isNonFinite(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	return s == "NaN" || s == "Infinity" || s == "-Infinity"
}

func isEmptyStructUnion(items []any) bool {
	if len(items) != 2 {
		return false
	}
	hasObject := false
	hasArray := false
	for _, item := range items {
		if m, ok := item.(jsonObject); ok {
			if m["type"] == "object" && m["properties"] == nil {
				hasObject = true
			}
			if m["type"] == "array" && m["items"] == nil {
				hasArray = true
			}
		}
	}
	return hasObject && hasArray
}

func canFlattenAllOf(allOf []jsonObject, parent jsonObject) bool {
	keys := make(map[string]bool)
	for k := range parent {
		if k != "allOf" {
			keys[k] = true
		}
	}
	for _, item := range allOf {
		for k := range item {
			if keys[k] {
				return false
			}
			keys[k] = true
		}
	}
	return true
}

func InlineRefs(value jsonObject) jsonObject {
	result := inlineRefs(value, nil, make(map[string]bool))
	if result == nil {
		return nil
	}
	return result.(jsonObject)
}

func inlineRefs(value any, defs jsonObject, seen map[string]bool) any {
	switch v := value.(type) {
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = inlineRefs(item, defs, seen)
		}
		return result
	case jsonObject:
		if defs == nil {
			if d, ok := v["$defs"].(jsonObject); ok {
				defs = d
			}
		}
		if ref, ok := v["$ref"].(string); ok && defs != nil {
			name := extractRefName(ref)
			if name != "" && !seen[name] {
				if target, ok := defs[name]; ok {
					next := make(jsonObject)
					maps.Copy(next, v)
					delete(next, "$ref")
					if t, ok := target.(jsonObject); ok {
						for k, tv := range t {
							if _, exists := next[k]; !exists {
								next[k] = tv
							}
						}
					}
					seen2 := make(map[string]bool)
					for k := range seen {
						seen2[k] = true
					}
					seen2[name] = true
					return inlineRefs(next, defs, seen2)
				}
			}
		}
		result := make(jsonObject, len(v))
		for k, item := range v {
			result[k] = inlineRefs(item, defs, seen)
		}
		return result
	default:
		return v
	}
}

func extractRefName(ref string) string {
	if after, ok := strings.CutPrefix(ref, "#/$defs/"); ok {
		return after
	}
	if after, ok := strings.CutPrefix(ref, "#/definitions/"); ok {
		return after
	}
	return ""
}

func DropDefsIfResolved(value jsonObject) jsonObject {
	result := dropDefsIfResolved(value)
	if result == nil {
		return nil
	}
	return result.(jsonObject)
}

func dropDefsIfResolved(value any) any {
	m, ok := value.(jsonObject)
	if !ok {
		return value
	}
	if hasLocalRef(m) {
		return value
	}
	result := make(jsonObject, len(m))
	for k, v := range m {
		if k == "$defs" || k == "definitions" {
			continue
		}
		result[k] = v
	}
	return result
}

func hasLocalRef(value any) bool {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if hasLocalRef(item) {
				return true
			}
		}
		return false
	case jsonObject:
		if ref, ok := v["$ref"].(string); ok {
			if strings.HasPrefix(ref, "#/$defs/") || strings.HasPrefix(ref, "#/definitions/") {
				return true
			}
		}
		for _, val := range v {
			if hasLocalRef(val) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
