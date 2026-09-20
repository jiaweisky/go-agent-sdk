package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func decodeJSONObject(data []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	return object, nil
}

func isJSONNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

// validateRequiredJSON checks that every field of the struct type whose json tag
// lacks "omitempty" is present and non-null in object. Required nested structs
// (and present optional ones) are validated recursively. Interface fields are
// not recursed: their concrete decode validates them.
func validateRequiredJSON(t reflect.Type, object map[string]json.RawMessage, skip map[string]bool) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, optional := jsonFieldName(field)
		if name == "" || skip[name] {
			continue
		}
		raw, ok := object[name]
		if !ok {
			if optional {
				continue
			}
			return fmt.Errorf("missing required field %q", name)
		}
		if isJSONNull(raw) {
			// Effect's optional/required wrappers reject null; only
			// Schema.Unknown (Go's empty interface) accepts it.
			if allowsJSONNull(field.Type) {
				continue
			}
			return fmt.Errorf("field %q must not be null", name)
		}
		if err := validateNestedJSON(field.Type, raw); err != nil {
			return err
		}
	}
	return nil
}

func validateNestedJSON(t reflect.Type, raw json.RawMessage) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		object, err := decodeJSONObject(raw)
		if err != nil {
			return err
		}
		return validateRequiredJSON(t, object, nil)
	case reflect.Map:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return fmt.Errorf("expected object: %w", err)
		}
		return nil
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("expected array: %w", err)
		}
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return nil
		}
		for _, item := range items {
			object, err := decodeJSONObject(item)
			if err != nil {
				return err
			}
			if err := validateRequiredJSON(elem, object, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			return name, true
		}
	}
	return name, false
}

// allowsJSONNull reports whether a field mirrors Schema.Unknown, which accepts
// an explicit null. Only the empty interface qualifies.
func allowsJSONNull(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && t.NumMethod() == 0
}
