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

// requiredJSONField returns a required field's non-null value, or an error when
// it is absent or null.
func requiredJSONField(object map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := object[name]
	if !ok || isJSONNull(raw) {
		return nil, fmt.Errorf("missing required field %q", name)
	}
	return raw, nil
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
		if err := validateJSONField(field, object, skip); err != nil {
			return err
		}
	}
	return nil
}

func validateJSONField(field reflect.StructField, object map[string]json.RawMessage, skip map[string]bool) error {
	name, optional := jsonFieldName(field)
	if name == "" || skip[name] {
		return nil
	}
	raw, ok := object[name]
	if !ok {
		if optional {
			return nil
		}
		return fmt.Errorf("missing required field %q", name)
	}
	if !isJSONNull(raw) {
		return validateNestedJSON(field.Type, raw)
	}
	// An explicit null is rejected for every field except an untyped value
	// (Go's empty interface).
	if allowsJSONNull(field.Type) {
		return nil
	}
	return fmt.Errorf("field %q must not be null", name)
}

func validateNestedJSON(t reflect.Type, raw json.RawMessage) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return validateJSONStruct(t, raw)
	case reflect.Map:
		if _, err := decodeJSONObject(raw); err != nil {
			return fmt.Errorf("expected object: %w", err)
		}
		return nil
	case reflect.Slice, reflect.Array:
		return validateJSONArray(t.Elem(), raw)
	}
	return nil
}

func validateJSONStruct(t reflect.Type, raw json.RawMessage) error {
	object, err := decodeJSONObject(raw)
	if err != nil {
		return err
	}
	return validateRequiredJSON(t, object, nil)
}

func validateJSONArray(elem reflect.Type, raw json.RawMessage) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("expected array: %w", err)
	}
	for elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	if elem.Kind() != reflect.Struct {
		return nil
	}
	for _, item := range items {
		if err := validateJSONStruct(elem, item); err != nil {
			return err
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

// allowsJSONNull reports whether a field accepts an explicit JSON null; only the
// empty interface does.
func allowsJSONNull(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && t.NumMethod() == 0
}
