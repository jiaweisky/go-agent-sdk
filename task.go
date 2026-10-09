package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/language"

	"github.com/jiaweisky/go-agent-sdk/internal/core/tool"
	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/jsonschema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TaskName is the task tool's registered name.
const TaskName = "task"

// TaskDescription is the task tool's description for the model.
//
//nolint:lll // prose lines in the description exceed 120 columns by design
const TaskDescription = `Launch a new agent to handle complex, multistep tasks autonomously.


When using the Task tool, you must specify a subagent_type parameter to select which agent type to use.

When NOT to use the Task tool:
- If you want to read a specific file path, use the Read or Glob tool instead of the Task tool, to find the match more quickly
- If you are searching for a specific class definition like "class Foo", use the Grep tool instead
- If you are searching for code within a specific file or set of 2-3 files, use the Read tool instead of the Task tool
- If no available agent is a good fit for the task, use other tools directly


Usage notes:
1. Launch multiple agents concurrently whenever possible, to maximize performance; to do that, use a single message with multiple tool uses
2. Once you have delegated work to an agent, do not duplicate that work yourself. Continue with non-overlapping tasks, or wait for the result. For background tasks, you will be notified automatically when the result is ready.
3. When the agent is done, it will return a single message back to you. The result returned by the agent is not visible to the user. To show the user the result, you should send a text message back to the user with a concise summary of the result. The output includes a task_id you can reuse later to continue the same subagent session.
4. Each agent invocation starts with a fresh context unless you provide task_id to resume the same subagent session (which continues with its previous messages and tool outputs). When starting fresh, your prompt should contain a highly detailed task description for the agent to perform autonomously and you should specify exactly what information the agent should return back to you in its final and only message to you.
5. The agent's outputs should generally be trusted
6. Clearly tell the agent whether you expect it to write code or just to do research (search, file reads, web fetches, etc.), since it is not aware of your user's intent. Tell it how to verify its work if possible (e.g., relevant test commands).
7. If the agent description mentions that it should be used proactively, then you should try your best to use it without the user having to ask for it first. Use your judgement.`

// TaskInputSchema is the task tool's parameters schema.
var TaskInputSchema = jsonschema.Object(
	jsonschema.Required(
		"description",
		jsonschema.String(jsonschema.Description("A short (3-5 words) description of the task")),
	),
	jsonschema.Required("prompt", jsonschema.String(jsonschema.Description("The task for the agent to perform"))),
	jsonschema.Required(
		"subagent_type",
		jsonschema.String(jsonschema.Description("The type of specialized agent to use for this task")),
	),
	jsonschema.Optional("task_id", jsonschema.String(jsonschema.Description(
		"This should only be set if you mean to resume a previous task (you can pass a prior task_id "+
			"and the task will continue the same subagent session as before instead of creating a fresh one)",
	))),
	jsonschema.Optional("command", jsonschema.String(jsonschema.Description("The command that triggered this task"))),
	jsonschema.Optional("background", jsonschema.Boolean(jsonschema.Description(
		"Run the agent in the background. You will be notified when it completes. "+
			"DO NOT sleep, poll, or proactively check on its progress",
	))),
)

// TaskOutputSchema is the task tool's structured output schema.
var TaskOutputSchema = jsonschema.Object(
	jsonschema.Required("title", jsonschema.String()),
	jsonschema.Required("sessionID", jsonschema.String()),
	jsonschema.Required("state", jsonschema.String()),
	jsonschema.Required("text", jsonschema.String()),
)

// TaskInput is the task tool's parameters.
type TaskInput struct {
	Description  string  `json:"description"`
	Prompt       string  `json:"prompt"`
	SubagentType string  `json:"subagent_type"`
	TaskID       *string `json:"task_id,omitempty"`
	Command      *string `json:"command,omitempty"`
	Background   *bool   `json:"background,omitempty"`
}

// TaskOutput is the tool's structured output.
type TaskOutput struct {
	Title     string `json:"title"`
	SessionID string `json:"sessionID"`
	State     string `json:"state"`
	Text      string `json:"text"`
}

// TaskRunInput is what the runner needs to spawn and run a subagent session.
type TaskRunInput struct {
	ParentSessionID string
	ParentAgent     string
	CallID          string
	SubagentType    string
	Description     string
	Prompt          string
	TaskID          *string
	// Files are media attachments admitted to the child session alongside the
	// prompt. The model-driven task tool leaves this empty (a model
	// cannot emit bytes); a host or structured caller may supply references.
	Files []schema.PromptFileAttachment
	// Progress publishes the running task state (durable) on the parent
	// session, carrying the child session id.
	Progress func(structured map[string]any, content []tool.Content) error
}

// TaskRunOutput is the subagent's result.
type TaskRunOutput struct {
	SessionID string
	Text      string
}

// TaskRunner spawns a subagent session and returns its final text.
type TaskRunner interface {
	RunTask(ctx context.Context, input TaskRunInput) (TaskRunOutput, error)
}

// TaskToolDeps are the task tool's dependencies.
type TaskToolDeps struct {
	Permission core.Permission
	Agents     *core.Agent
	Runner     TaskRunner
	// AgentPermissions are the calling agent's rules, used to filter the
	// advertised subagent types.
	AgentPermissions schema.PermissionRuleset
	// Locale orders the advertised subagent types.
	Locale language.Tag
}

// SubagentTypes lists the agents the caller may delegate to: those whose mode is
// not "primary" and whose "task" permission is not denied, sorted by name with the
// given locale.
func SubagentTypes(agents *core.Agent, permissions schema.PermissionRuleset, locale language.Tag) []schema.AgentInfo {
	var items []schema.AgentInfo
	for _, item := range agents.All() {
		if item.Mode == schema.AgentModePrimary {
			continue
		}
		if core.PermissionEvaluate(TaskName, string(item.ID), permissions).Effect == schema.PermissionEffectDeny {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return util.CompareLocale(string(items[i].ID), string(items[j].ID), locale) < 0
	})
	return items
}

func taskDescription(agents *core.Agent, permissions schema.PermissionRuleset, locale language.Tag) string {
	lines := []string{"Available agent types and the tools they have access to:"}
	for _, item := range SubagentTypes(agents, permissions, locale) {
		description := "This subagent should only be called manually by the user."
		if item.Description != nil && *item.Description != "" {
			description = *item.Description
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", item.ID, description))
	}
	return strings.Join([]string{TaskDescription, strings.Join(lines, "\n")}, "\n")
}

func renderTaskOutput(output TaskOutput) string {
	tag := "task_result"
	if output.State == "error" {
		tag = "task_error"
	}
	return strings.Join([]string{
		fmt.Sprintf(`<task id="%s" state="%s">`, output.SessionID, output.State),
		"<" + tag + ">",
		output.Text,
		"</" + tag + ">",
		"</task>",
	}, "\n")
}

// MakeTask builds the task tool from deps.
func MakeTask(deps TaskToolDeps) *tool.Def[TaskInput, TaskOutput, TaskOutput] {
	return tool.Make(tool.Config[TaskInput, TaskOutput, TaskOutput]{
		Description: taskDescription(deps.Agents, deps.AgentPermissions, deps.Locale),
		Input:       TaskInputSchema,
		Output:      TaskOutputSchema,
		ToModelOutput: func(input TaskInput, output any) []tool.Content {
			return []tool.Content{tool.TextContent{Text: renderTaskOutput(tool.DecodeStructured[TaskOutput](output))}}
		},
		Execute: func(execCtx context.Context, input TaskInput, ctx tool.Context) (TaskOutput, error) {
			if input.Background != nil && *input.Background {
				return TaskOutput{}, &tool.Failure{Message: "background subagents are not supported"}
			}
			source := &core.PermissionSource{
				Type:      core.PermissionSourceTypeTool,
				MessageID: ctx.AssistantMessageID,
				CallID:    ctx.ToolCallID,
			}
			if err := deps.Permission.Assert(core.PermissionAssertInput{
				SessionID: ctx.SessionID,
				Action:    TaskName,
				Resources: []string{input.SubagentType},
				Save:      []string{"*"},
				Source:    source,
				Agent:     &ctx.Agent,
			}); err != nil {
				return TaskOutput{}, err
			}
			result, err := deps.Runner.RunTask(execCtx, TaskRunInput{
				ParentSessionID: ctx.SessionID,
				ParentAgent:     ctx.Agent,
				CallID:          ctx.ToolCallID,
				SubagentType:    input.SubagentType,
				Description:     input.Description,
				Prompt:          input.Prompt,
				TaskID:          input.TaskID,
				Progress:        ctx.Progress,
			})
			if err != nil {
				return TaskOutput{}, &tool.Failure{Message: err.Error()}
			}
			return TaskOutput{
				Title:     input.Description,
				SessionID: result.SessionID,
				State:     "completed",
				Text:      result.Text,
			}, nil
		},
	})
}

// RegisterTask registers the task tool.
func RegisterTask(tools tool.Tools, deps TaskToolDeps) (func(), error) {
	return tools.Register(map[string]tool.Tool{TaskName: MakeTask(deps)})
}

// DeriveSubagentSessionPermission combines the parent session's deny and
// external_directory rules with default todowrite/task denies, unless the
// subagent's own ruleset already defines a rule for them.
func DeriveSubagentSessionPermission(
	parent schema.PermissionRuleset,
	subagent schema.AgentInfo,
) schema.PermissionRuleset {
	hasTaskRule := hasPermissionAction(subagent.Permissions, TaskName)
	hasTodoRule := hasPermissionAction(subagent.Permissions, "todowrite")
	result := make(schema.PermissionRuleset, 0, len(parent)+2)
	for _, rule := range parent {
		if rule.Action == "external_directory" || rule.Effect == schema.PermissionEffectDeny {
			result = append(result, rule)
		}
	}
	if !hasTodoRule {
		result = append(
			result,
			schema.PermissionRule{Action: "todowrite", Resource: "*", Effect: schema.PermissionEffectDeny},
		)
	}
	if !hasTaskRule {
		result = append(result, schema.PermissionRule{Action: TaskName, Resource: "*", Effect: schema.PermissionEffectDeny})
	}
	return result
}

// hasPermissionAction reports whether rules mention action at all, regardless
// of effect.
func hasPermissionAction(rules schema.PermissionRuleset, action string) bool {
	for _, rule := range rules {
		if rule.Action == action {
			return true
		}
	}
	return false
}
