package jsonschema

import (
	"encoding/json"

	jsonschema6 "github.com/santhosh-tekuri/jsonschema/v6"
)

// CompileValidation compiles a Validation projection into a runnable schema.
// It returns nil for a nil projection.
func CompileValidation(validation map[string]any) (*jsonschema6.Schema, error) {
	if validation == nil {
		return nil, nil
	}
	data, err := json.Marshal(validation)
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	compiler := jsonschema6.NewCompiler()
	if err := compiler.AddResource("schema.json", document); err != nil {
		return nil, err
	}
	return compiler.Compile("schema.json")
}
