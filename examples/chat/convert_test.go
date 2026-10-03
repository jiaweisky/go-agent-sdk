package main

import (
	"context"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk"
)

// TestConvertLengthTool guards the typed handler and its unit validation.
func TestConvertLengthTool(t *testing.T) {
	tool, ok := runtimeTool(convertLengthName)
	if !ok {
		t.Fatalf("runtime tool %s missing", convertLengthName)
	}
	out, err := tool.Handler(context.Background(),
		map[string]any{"value": float64(1000), "from": "m", "to": "km"}, agent.ToolContext{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out != "1000 m = 1 km" {
		t.Fatalf("out = %v, want %q", out, "1000 m = 1 km")
	}
	out, err = tool.Handler(context.Background(),
		map[string]any{"value": float64(10), "from": "cm", "to": "m"}, agent.ToolContext{})
	if err != nil {
		t.Fatalf("handler (cm): %v", err)
	}
	if out != "10 cm = 0.1 m" {
		t.Fatalf("out = %v, want %q", out, "10 cm = 0.1 m")
	}
	if _, err := tool.Handler(context.Background(),
		map[string]any{"value": float64(1), "from": "m", "to": "yard"}, agent.ToolContext{}); err == nil {
		t.Fatal("an unknown unit should error")
	}
}

// TestConvertLengthSchema guards that the advertised schema really carries the
// enum and required constraints (the point of the typed tool).
func TestConvertLengthSchema(t *testing.T) {
	tool, ok := runtimeTool(convertLengthName)
	if !ok {
		t.Fatalf("runtime tool %s missing", convertLengthName)
	}
	model := tool.Parameters.Model()
	props, _ := model["properties"].(map[string]any)
	from, _ := props["from"].(map[string]any)
	if values, _ := from["enum"].([]any); len(values) != len(lengthUnits) {
		t.Fatalf("from enum = %v, want %d units", from["enum"], len(lengthUnits))
	}
	if required, _ := model["required"].([]any); len(required) != 3 {
		t.Fatalf("required = %v, want value/from/to", model["required"])
	}
}

// TestListUnitsToolSkipsPermission guards the read-only companion: same action,
// but SkipPermission means it never prompts.
func TestListUnitsToolSkipsPermission(t *testing.T) {
	tool, ok := runtimeTool("list_length_units")
	if !ok {
		t.Fatal("list_length_units missing")
	}
	if !tool.SkipPermission {
		t.Fatal("list_length_units should skip permission")
	}
	if tool.Permission != convertLengthName {
		t.Fatalf("permission = %q, want %q", tool.Permission, convertLengthName)
	}
}

// TestRuntimeToolNames guards that every runtime tool is listed.
func TestRuntimeToolNames(t *testing.T) {
	names := strings.Join(runtimeToolNames(), ",")
	for _, want := range []string{"convert_length", "dice", "list_length_units", "now"} {
		if !strings.Contains(names, want) {
			t.Fatalf("names = %q, missing %q", names, want)
		}
	}
}

// TestToolHintMentionsAvailable guards that /tool's empty hint is derived from
// the registry, so adding a tool cannot leave the hint stale.
func TestToolHintMentionsAvailable(t *testing.T) {
	out := runCommand(t, nil, func(c *chat) { c.toolsCommand("/tool") })
	if !strings.Contains(out, "convert_length") {
		t.Fatalf("out = %q, want the available tools in the hint", out)
	}
}
