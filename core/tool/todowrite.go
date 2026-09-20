package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/schema"
	rootschema "github.com/user/go-agent-sdk/schema"
)

const TodoWriteName = "todowrite"

const TodoWriteDescription = "Create and maintain a structured task list for the current coding session. " +
	"Use it to track progress during multi-step work and keep todo statuses current."

var todoWriteInfoSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"content": map[string]any{
			"type":        "string",
			"description": "Brief description of the task",
		},
		"status": map[string]any{
			"type":        "string",
			"description": "Current status of the task: pending, in_progress, completed, cancelled",
		},
		"priority": map[string]any{
			"type":        "string",
			"description": "Priority level of the task: high, medium, low",
		},
	},
	"required": []any{"content", "status", "priority"},
}

var TodoWriteInputSchema = schema.JsonSchema{
	"type": "object",
	"properties": map[string]any{
		"todos": map[string]any{
			"type":        "array",
			"description": "The updated todo list",
			"items":       todoWriteInfoSchema,
		},
	},
	"required": []any{"todos"},
}

var TodoWriteOutputSchema = schema.JsonSchema{
	"type": "object",
	"properties": map[string]any{
		"todos": map[string]any{
			"type":  "array",
			"items": todoWriteInfoSchema,
		},
	},
	"required": []any{"todos"},
}

type TodoWriteInput struct {
	Todos []rootschema.SessionTodoInfo `json:"todos"`
}

type TodoWriteOutput struct {
	Todos []rootschema.SessionTodoInfo `json:"todos"`
}

// TodoUpdater is the narrow session-todo surface the tool depends on.
type TodoUpdater interface {
	Update(sessionID rootschema.SessionID, todos []rootschema.SessionTodoInfo) error
}

type TodoWriteDeps struct {
	Todos      TodoUpdater
	Permission core.Permission
}

// TodoWriteToModelOutput mirrors TodoWriteTool.toModelOutput: JSON.stringify with
// a two-space indent and no HTML escaping.
func TodoWriteToModelOutput(output TodoWriteOutput) string {
	todos := output.Todos
	if todos == nil {
		todos = []rootschema.SessionTodoInfo{}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(todos); err != nil {
		return "[]"
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}

func MakeTodoWrite(deps TodoWriteDeps) *Def[TodoWriteInput, TodoWriteOutput, TodoWriteOutput] {
	return Make(Config[TodoWriteInput, TodoWriteOutput, TodoWriteOutput]{
		Description: TodoWriteDescription,
		Input:       TodoWriteInputSchema,
		Output:      TodoWriteOutputSchema,
		ToModelOutput: func(input TodoWriteInput, output any) []Content {
			var out TodoWriteOutput
			if b, err := json.Marshal(output); err == nil {
				json.Unmarshal(b, &out)
			}
			return []Content{TextContent{Text: TodoWriteToModelOutput(out)}}
		},
		Execute: func(execCtx context.Context, input TodoWriteInput, ctx Context) (TodoWriteOutput, error) {
			if err := execCtx.Err(); err != nil {
				return TodoWriteOutput{}, err
			}
			source := &core.PermissionSource{
				Type:      core.PermissionSourceTypeTool,
				MessageID: ctx.AssistantMessageID,
				CallID:    ctx.ToolCallID,
			}
			if err := deps.Permission.Assert(core.PermissionAssertInput{
				SessionID: ctx.SessionID,
				Action:    TodoWriteName,
				Resources: []string{"*"},
				Save:      []string{"*"},
				Source:    source,
				Agent:     &ctx.Agent,
			}); err != nil {
				return TodoWriteOutput{}, &Failure{Message: "Unable to update todos"}
			}
			if err := deps.Todos.Update(rootschema.SessionID(ctx.SessionID), input.Todos); err != nil {
				return TodoWriteOutput{}, &Failure{Message: "Unable to update todos"}
			}
			return TodoWriteOutput{Todos: input.Todos}, nil
		},
	})
}

func RegisterTodoWrite(registry *Registry, deps TodoWriteDeps) (func(), error) {
	return registry.Register(map[string]Tool{
		TodoWriteName: MakeTodoWrite(deps),
	})
}
