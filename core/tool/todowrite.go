package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/jsonschema"
	rootschema "github.com/user/go-agent-sdk/schema"
)

const TodoWriteName = "todowrite"

const TodoWriteDescription = "Create and maintain a structured task list for the current coding session. " +
	"Use it to track progress during multi-step work and keep todo statuses current."

var todoWriteInfoSchema = jsonschema.Object(
	jsonschema.Required("content", jsonschema.String(jsonschema.Description("Brief description of the task"))),
	jsonschema.Required("status", jsonschema.String(jsonschema.Description("Current status of the task: pending, in_progress, completed, cancelled"))),
	jsonschema.Required("priority", jsonschema.String(jsonschema.Description("Priority level of the task: high, medium, low"))),
)

var TodoWriteInputSchema = jsonschema.Object(
	jsonschema.Required("todos", jsonschema.Array(todoWriteInfoSchema, jsonschema.Description("The updated todo list"))),
)

var TodoWriteOutputSchema = jsonschema.Object(
	jsonschema.Required("todos", jsonschema.Array(todoWriteInfoSchema)),
)

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

// TodoWriteToModelOutput renders the todo list as two-space-indented JSON, without
// HTML escaping.
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
				return TodoWriteOutput{}, failure("Unable to update todos", err)
			}
			if err := deps.Todos.Update(rootschema.SessionID(ctx.SessionID), input.Todos); err != nil {
				return TodoWriteOutput{}, failure("Unable to update todos", err)
			}
			return TodoWriteOutput{Todos: input.Todos}, nil
		},
	})
}

func RegisterTodoWrite(tools Tools, deps TodoWriteDeps) (func(), error) {
	return tools.Register(map[string]Tool{
		TodoWriteName: MakeTodoWrite(deps),
	})
}
