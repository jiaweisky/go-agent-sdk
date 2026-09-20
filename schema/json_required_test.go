package schema

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestValidateRequiredJSONAssertsContainers(t *testing.T) {
	type inner struct {
		Values []string       `json:"values"`
		Record map[string]any `json:"record"`
	}
	type outer struct {
		Inner inner `json:"inner"`
	}
	validate := func(innerJSON string) error {
		object := map[string]json.RawMessage{"inner": json.RawMessage(innerJSON)}
		return validateRequiredJSON(reflect.TypeOf(outer{}), object, nil)
	}

	if err := validate(`{"values":[],"record":{}}`); err != nil {
		t.Fatalf("valid containers failed: %v", err)
	}
	if err := validate(`{"values":{},"record":{}}`); err == nil {
		t.Fatalf("expected error for non-array slice")
	}
	if err := validate(`{"values":[],"record":[]}`); err == nil {
		t.Fatalf("expected error for non-object map")
	}
	if err := validate(`{"values":null,"record":{}}`); err == nil {
		t.Fatalf("expected error for null slice")
	}
}
