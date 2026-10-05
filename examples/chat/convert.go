package main

import (
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/jsonschema"
)

// convertLengthName is the convert_length tool's name; it is also the permission
// action the length tools share.
const convertLengthName = "convert_length"

// lengthUnit is one supported unit and its size in meters.
type lengthUnit struct {
	name   string
	meters float64
}

// lengthUnits are the supported units, in display order.
var lengthUnits = []lengthUnit{
	{"mm", 0.001}, {"cm", 0.01}, {"m", 1}, {"km", 1000},
	{"in", 0.0254}, {"ft", 0.3048}, {"yd", 0.9144}, {"mi", 1609.344},
}

// lengthFactor returns a unit's size in meters.
func lengthFactor(name string) (float64, bool) {
	for _, unit := range lengthUnits {
		if unit.name == name {
			return unit.meters, true
		}
	}
	return 0, false
}

// unitNames lists the supported unit names, in display order.
func unitNames() []string {
	names := make([]string, len(lengthUnits))
	for i, unit := range lengthUnits {
		names[i] = unit.name
	}
	return names
}

// convertLengthTool converts a length between units. Its parameters are authored
// as a schema AST rather than reflected, so "from" and "to" are enums; the
// handler stays typed. The custom permission action routes the model's call
// through the interactive approval flow (when the permission plugin's rule is
// active, i.e. not --yolo).
func convertLengthTool() agent.Tool {
	tool := agent.DefineTool(convertLengthName, "Convert a length between units.",
		func(input convertLengthInput, _ agent.ToolContext) (string, error) {
			from, ok := lengthFactor(input.From)
			if !ok {
				return "", fmt.Errorf("unknown unit: %s", input.From)
			}
			to, ok := lengthFactor(input.To)
			if !ok {
				return "", fmt.Errorf("unknown unit: %s", input.To)
			}
			return fmt.Sprintf("%g %s = %g %s", input.Value, input.From, input.Value*from/to, input.To), nil
		})
	tool.Parameters = jsonschema.Object(
		jsonschema.Required("value", jsonschema.Number(jsonschema.Description("Length to convert"))),
		jsonschema.Required("from", unitEnum("Source unit")),
		jsonschema.Required("to", unitEnum("Target unit")),
	)
	tool.Permission = convertLengthName
	return tool
}

// listUnitsTool is the read-only companion: it declares the same permission
// action but skips the check, so it never prompts.
func listUnitsTool() agent.Tool {
	tool := agent.DefineTool("list_length_units", "List the units convert_length supports.",
		func(_ struct{}, _ agent.ToolContext) (string, error) {
			return "supported units: " + strings.Join(unitNames(), ", "), nil
		})
	tool.Permission = convertLengthName
	tool.SkipPermission = true
	return tool
}

type convertLengthInput struct {
	Value float64 `json:"value"`
	From  string  `json:"from"`
	To    string  `json:"to"`
}

// unitEnum is a string enum of the supported length units.
func unitEnum(description string) *jsonschema.Schema {
	names := unitNames()
	units := make([]any, len(names))
	for i, name := range names {
		units[i] = name
	}
	return jsonschema.Enum(units, jsonschema.Description(description))
}
