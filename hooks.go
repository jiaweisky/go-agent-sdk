package agent

import (
	"context"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/core/tool"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
)

// SessionHooks intercepts session lifecycle and tool execution.
type SessionHooks struct {
	// OnPreToolUse runs before each tool execution. It can allow/deny the call
	// or rewrite its arguments.
	OnPreToolUse func(PreToolUseInput) (*PreToolUseOutput, error)
	// OnPostToolUse runs after a successful tool execution.
	OnPostToolUse func(PostToolUseInput) (*PostToolUseOutput, error)
	// OnPostToolUseFailure runs after a failed tool execution.
	OnPostToolUseFailure func(PostToolUseFailureInput) (*PostToolUseFailureOutput, error)
	// OnToolDefinition runs before tool definitions are sent to the model; it
	// can rewrite a tool's description, input schema or metadata.
	OnToolDefinition func(ToolDefinitionInput) (*ToolDefinitionOutput, error)
	// OnUserPromptSubmitted runs before a prompt is admitted; it can rewrite it.
	OnUserPromptSubmitted func(UserPromptSubmittedInput) (*UserPromptSubmittedOutput, error)
	// OnSessionStart runs when a session is created or resumed. An error aborts
	// the call.
	OnSessionStart func(SessionStartInput) (*SessionStartOutput, error)
	// OnSessionEnd runs when a session is closed.
	OnSessionEnd func(SessionEndInput) (*SessionEndOutput, error)
	// OnErrorOccurred runs when a turn fails.
	OnErrorOccurred func(ErrorOccurredInput) (*ErrorOccurredOutput, error)
}

// PreToolUseInput describes a tool call about to run.
type PreToolUseInput struct {
	SessionID  string
	ToolCallID string
	ToolName   string
	Args       any
}

// PreToolUseOutput can deny a call or rewrite its arguments.
type PreToolUseOutput struct {
	// PermissionDecision may be "deny" to block the call; any other value
	// (including empty) allows it. Permission is asserted separately, before
	// this hook runs.
	PermissionDecision string
	ModifiedArgs       any
	AdditionalContext  string
}

// PostToolUseInput describes a completed tool call.
type PostToolUseInput struct {
	SessionID  string
	ToolCallID string
	ToolName   string
	Args       any
	Result     any
}

// PostToolUseOutput can add context to the model.
type PostToolUseOutput struct {
	AdditionalContext string
}

// PostToolUseFailureInput describes a failed tool call.
type PostToolUseFailureInput struct {
	SessionID  string
	ToolCallID string
	ToolName   string
	Args       any
	Error      string
}

// PostToolUseFailureOutput can add retry guidance to the model.
type PostToolUseFailureOutput struct {
	AdditionalContext string
}

// ToolDefinitionInput describes a tool definition about to be sent to the model.
type ToolDefinitionInput struct {
	SessionID  string
	ToolID     string
	Definition llmschema.ToolDefinition
}

// ToolDefinitionOutput replaces the model-visible tool definition.
type ToolDefinitionOutput struct {
	Definition llmschema.ToolDefinition
}

// UserPromptSubmittedInput describes a prompt before admission.
type UserPromptSubmittedInput struct {
	SessionID string
	Prompt    string
}

// UserPromptSubmittedOutput can rewrite the prompt.
type UserPromptSubmittedOutput struct {
	ModifiedPrompt string
}

// SessionStartInput describes session creation.
type SessionStartInput struct {
	SessionID string
	Agent     string
	Model     string
}

// SessionStartOutput can add session initialization context.
type SessionStartOutput struct {
	AdditionalContext string
}

// SessionEndInput describes session closure.
type SessionEndInput struct {
	SessionID string
	Reason    string
}

// SessionEndOutput is reserved for future use.
type SessionEndOutput struct{}

// ErrorOccurredInput describes a failed turn.
type ErrorOccurredInput struct {
	SessionID string
	Error     string
}

// ErrorOccurredOutput chooses how to handle the error: "abort" (default).
type ErrorOccurredOutput struct {
	ErrorHandling string
}

// toolMaterializer is the session-aware materialization surface.
type toolMaterializer interface {
	MaterializeFor(sessionID string, permissions []core.PermissionRule) (*tool.Materialization, error)
}

// hookedRegistry wraps a tool materializer so tool hooks fire around settlement.
type hookedRegistry struct {
	inner    toolMaterializer
	hooksFor func(sessionID string) *SessionHooks
}

func (h hookedRegistry) MaterializeFor(
	sessionID string,
	permissions []core.PermissionRule,
) (*tool.Materialization, error) {
	mat, err := h.inner.MaterializeFor(sessionID, permissions)
	if err != nil {
		return nil, err
	}
	if mat == nil {
		mat = &tool.Materialization{}
	}
	if err := h.applyToolDefinitions(sessionID, mat); err != nil {
		return nil, err
	}
	inner := mat.Settle
	if inner == nil {
		// The session has no registry (closed or unknown). Return an error
		// settlement rather than a nil Settle the runner would call.
		mat.Settle = func(context.Context, tool.ExecuteInput) (*tool.Settlement, error) {
			return hookError("unknown session: " + sessionID), nil
		}
		return mat, nil
	}
	mat.Settle = func(ctx context.Context, input tool.ExecuteInput) (*tool.Settlement, error) {
		hooks := h.hooksForSession(input.SessionID)
		if hooks == nil {
			return inner(ctx, input)
		}
		preContext, denied := applyPreToolUse(hooks, &input)
		if denied != nil {
			return denied, nil
		}
		settlement, err := inner(ctx, input)
		if err != nil {
			return nil, err
		}
		applyPostToolUse(hooks, input, settlement)
		appendSettlementContext(settlement, preContext)
		return settlement, nil
	}
	return mat, nil
}

// applyToolDefinitions lets OnToolDefinition rewrite the model-visible tool
// definitions before they are sent.
func (h hookedRegistry) applyToolDefinitions(sessionID string, mat *tool.Materialization) error {
	hooks := h.hooksForSession(sessionID)
	if hooks == nil || hooks.OnToolDefinition == nil {
		return nil
	}
	for i := range mat.Definitions {
		out, err := hooks.OnToolDefinition(ToolDefinitionInput{
			SessionID:  sessionID,
			ToolID:     mat.Definitions[i].Name,
			Definition: mat.Definitions[i],
		})
		if err != nil {
			return err
		}
		if out != nil {
			mat.Definitions[i] = out.Definition
		}
	}
	return nil
}

// hooksForSession returns the session's hooks, or nil when none are registered.
func (h hookedRegistry) hooksForSession(sessionID string) *SessionHooks {
	if h.hooksFor == nil {
		return nil
	}
	return h.hooksFor(sessionID)
}

// applyPreToolUse runs OnPreToolUse, letting it rewrite the call's arguments. It
// returns the hook's additional context and, when the call must not run, the
// settlement to return instead (a denial or a hook failure).
func applyPreToolUse(hooks *SessionHooks, input *tool.ExecuteInput) (string, *tool.Settlement) {
	if hooks.OnPreToolUse == nil {
		return "", nil
	}
	out, err := hooks.OnPreToolUse(PreToolUseInput{
		SessionID: input.SessionID, ToolCallID: input.Call.ID,
		ToolName: input.Call.Name, Args: input.Call.Input,
	})
	if err != nil {
		return "", hookError(err.Error())
	}
	if out == nil {
		return "", nil
	}
	if out.PermissionDecision == "deny" {
		return "", hookError("Tool call denied by hook: " + input.Call.Name)
	}
	if out.ModifiedArgs != nil {
		input.Call.Input = out.ModifiedArgs
	}
	return out.AdditionalContext, nil
}

// applyPostToolUse runs OnPostToolUse (or OnPostToolUseFailure) and appends the
// hook's context to the settlement.
func applyPostToolUse(hooks *SessionHooks, input tool.ExecuteInput, settlement *tool.Settlement) {
	additional := postToolUseContext(hooks, input, settlement)
	appendSettlementContext(settlement, additional)
}

// postToolUseContext runs the success or failure post-tool hook and returns its
// additional context.
func postToolUseContext(hooks *SessionHooks, input tool.ExecuteInput, settlement *tool.Settlement) string {
	if settlement.Result.Type == "error" {
		if hooks.OnPostToolUseFailure == nil {
			return ""
		}
		out, _ := hooks.OnPostToolUseFailure(PostToolUseFailureInput{
			SessionID: input.SessionID, ToolCallID: input.Call.ID, ToolName: input.Call.Name,
			Args: input.Call.Input, Error: ToolOutputText(settlement.Result.Value),
		})
		if out != nil {
			return out.AdditionalContext
		}
		return ""
	}
	if hooks.OnPostToolUse == nil {
		return ""
	}
	out, _ := hooks.OnPostToolUse(PostToolUseInput{
		SessionID: input.SessionID, ToolCallID: input.Call.ID, ToolName: input.Call.Name,
		Args: input.Call.Input, Result: settlement.Result.Value,
	})
	if out != nil {
		return out.AdditionalContext
	}
	return ""
}

// appendSettlementContext adds hook-provided context to a tool result so it
// reaches the model.
func appendSettlementContext(settlement *tool.Settlement, text string) {
	if settlement == nil || text == "" || settlement.Output == nil {
		return
	}
	settlement.Output.Content = append(settlement.Output.Content, llmschema.ToolTextContent{Type: "text", Text: text})
}

func hookError(message string) *tool.Settlement {
	return &tool.Settlement{Result: llmschema.ToolResultValue{Type: "error", Value: message}}
}
