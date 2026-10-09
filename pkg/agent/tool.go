package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/internal/core/tool"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
)

// ToolContext describes a single tool invocation.
type ToolContext struct {
	SessionID  string
	ToolCallID string
	// Progress reports running-tool state from the handler. structured is
	// surfaced to subscribers as ToolProgress.Structured; content replaces the
	// running tool's content parts. It is nil when the tool has no publication
	// context.
	Progress func(structured map[string]any, content []tool.Content) error
}

// Tool is a user-defined tool exposed to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  *jsonschema.Schema
	Handler     func(ctx context.Context, input any, tc ToolContext) (any, error)
	// Permission overrides the permission action; it defaults to Name.
	Permission string
	// SkipPermission bypasses the permission check for this tool.
	SkipPermission bool
}

// DefineTool builds a Tool whose parameters schema is generated from In via
// reflection, with a typed handler.
//
//	type LookupParams struct {
//	    ID string `json:"id" jsonschema:"Issue identifier"`
//	}
//
//	lookup := agent.DefineTool("lookup", "Look up an issue",
//	    func(params LookupParams, tc agent.ToolContext) (string, error) {
//	        return fetch(params.ID), nil
//	    })
func DefineTool[In, Out any](name, description string, handler func(In, ToolContext) (Out, error)) Tool {
	return Tool{
		Name:        name,
		Description: description,
		Parameters:  SchemaForType(reflect.TypeOf((*In)(nil)).Elem()),
		Handler: func(ctx context.Context, input any, tc ToolContext) (any, error) {
			var typed In
			data, err := json.Marshal(input)
			if err != nil {
				return nil, fmt.Errorf("tool %s: marshal arguments: %w", name, err)
			}
			if err := json.Unmarshal(data, &typed); err != nil {
				return nil, fmt.Errorf("tool %s: unmarshal arguments into %T: %w", name, typed, err)
			}
			return handler(typed, tc)
		},
	}
}

// SchemaForType generates a JSON Schema for a Go type. Unknown kinds fall back
// to a permissive schema.
func SchemaForType(t reflect.Type) *jsonschema.Schema {
	if t == nil {
		return jsonschema.Raw(map[string]any{})
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return jsonschema.String()
	case reflect.Bool:
		return jsonschema.Boolean()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return jsonschema.Integer()
	case reflect.Float32, reflect.Float64:
		return jsonschema.Number()
	case reflect.Slice, reflect.Array:
		return jsonschema.Array(SchemaForType(t.Elem()))
	case reflect.Struct:
		return schemaForStruct(t)
	default:
		return jsonschema.Raw(map[string]any{})
	}
}

// schemaForStruct builds an object schema from a struct's exported, non-ignored
// fields.
func schemaForStruct(t reflect.Type) *jsonschema.Schema {
	fields := make([]jsonschema.Field, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, opts := parseJSONTag(field.Tag.Get("json"), field.Name)
		if name == "-" {
			continue
		}
		child := SchemaForType(field.Type)
		if description := descriptionOf(field); description != "" {
			child = child.With(jsonschema.Description(description))
		}
		if strings.Contains(opts, "omitempty") || field.Type.Kind() == reflect.Pointer {
			fields = append(fields, jsonschema.Optional(name, child))
		} else {
			fields = append(fields, jsonschema.Required(name, child))
		}
	}
	return jsonschema.Object(fields...)
}

// parseJSONTag returns a struct field's JSON name and remaining tag options,
// falling back to the Go field name when the tag is absent.
func parseJSONTag(tag, fallback string) (string, string) {
	if tag == "" {
		return fallback, ""
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = fallback
	}
	return name, strings.Join(parts[1:], ",")
}

// descriptionOf returns a field's description from its jsonschema or
// description tag.
func descriptionOf(field reflect.StructField) string {
	if value := field.Tag.Get("jsonschema"); value != "" {
		return value
	}
	return field.Tag.Get("description")
}

// coreTool converts a user Tool into the core tool representation, asserting
// the permission action before invoking the handler unless SkipPermission is
// set.
func (t Tool) coreTool(permission core.Permission) tool.Tool {
	def := tool.Make(tool.Config[map[string]any, any, any]{
		Description: t.Description,
		Input:       t.Parameters,
		Output:      jsonschema.Raw(map[string]any{}),
		Execute: func(ctx context.Context, input map[string]any, tctx tool.Context) (any, error) {
			if !t.SkipPermission && permission != nil {
				action := t.Permission
				if action == "" {
					action = t.Name
				}
				if err := permission.Assert(core.PermissionAssertInput{
					SessionID: tctx.SessionID,
					Action:    action,
					Resources: []string{"*"},
					Save:      []string{"*"},
					Agent:     &tctx.Agent,
					Source: &core.PermissionSource{
						Type:      core.PermissionSourceTypeTool,
						MessageID: tctx.AssistantMessageID,
						CallID:    tctx.ToolCallID,
					},
				}); err != nil {
					return nil, err
				}
			}
			return t.Handler(ctx, input, ToolContext{
				SessionID:  tctx.SessionID,
				ToolCallID: tctx.ToolCallID,
				Progress:   tctx.Progress,
			})
		},
		ToModelOutput: func(input map[string]any, output any) []tool.Content {
			return []tool.Content{tool.TextContent{Text: ToolOutputText(output)}}
		},
	})
	if t.Permission != "" {
		def = tool.WithPermission(def, t.Permission)
	}
	return def
}

// ToolOutputText renders a tool result for the model: strings pass through,
// everything else is JSON-encoded.
func ToolOutputText(output any) string {
	if output == nil {
		return ""
	}
	if text, ok := output.(string); ok {
		return text
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprint(output)
	}
	return string(data)
}
