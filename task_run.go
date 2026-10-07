package agent

import (
	"context"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// deferredTaskRunner defers to the runtime's task runner. The builtin deps are
// assembled before the runtime's coordinator exists, so the task tool cannot
// hold the runner directly.
type deferredTaskRunner struct{ rt *runtime }

func (t *deferredTaskRunner) RunTask(ctx context.Context, input TaskRunInput) (TaskRunOutput, error) {
	return t.rt.runTask(ctx, input)
}

func (r *runtime) runTask(ctx context.Context, input TaskRunInput) (TaskRunOutput, error) {
	parent, err := r.store.Get(schema.SessionID(input.ParentSessionID))
	if err != nil {
		return TaskRunOutput{}, err
	}
	if parent == nil {
		return TaskRunOutput{}, fmt.Errorf("parent session not found: %s", input.ParentSessionID)
	}
	subagent, derived, err := r.resolveSubagent(parent, input)
	if err != nil {
		return TaskRunOutput{}, err
	}

	childID, reused, err := r.resolveChildSession(input, parent, subagent, derived)
	if err != nil {
		return TaskRunOutput{}, err
	}
	childAgent, childRules := r.childAgentAndRules(childID, reused, subagent, derived)
	registry, err := r.newSessionRegistry(childAgent, childRules, nil)
	if err != nil {
		return TaskRunOutput{}, err
	}
	if err := r.setSessionRegistry(string(childID), registry); err != nil {
		return TaskRunOutput{}, err
	}
	defer r.dispatcher.remove(string(childID))
	// Subagent tool calls inherit the parent session's hooks and handlers.
	r.inheritSession(parent.ID, childID)
	defer r.clearSession(childID)

	r.publishTaskProgress(input, childID, subagent, parent)
	if err := r.runChildSession(ctx, childID, input.Prompt); err != nil {
		return TaskRunOutput{}, err
	}
	text, err := r.lastAssistantText(childID)
	if err != nil {
		return TaskRunOutput{}, err
	}
	return TaskRunOutput{SessionID: string(childID), Text: text}, nil
}

// resolveSubagent checks the nesting limit and returns the subagent to run,
// together with the permission ruleset its session starts from.
func (r *runtime) resolveSubagent(
	parent *schema.SessionInfo,
	input TaskRunInput,
) (*schema.AgentInfo, schema.PermissionRuleset, error) {
	if err := r.checkSubagentDepth(parent); err != nil {
		return nil, nil, err
	}
	subagent := r.agents.Get(schema.AgentID(input.SubagentType))
	if subagent == nil {
		return nil, nil, fmt.Errorf("Unknown agent type: %s is not a valid agent type", input.SubagentType)
	}
	return subagent, DeriveSubagentSessionPermission(permissionRulesOf(parent), *subagent), nil
}

// childAgentAndRules is the agent and permission ruleset a child session runs
// with. A resumed session keeps the agent and ruleset it was created with.
func (r *runtime) childAgentAndRules(
	childID schema.SessionID,
	reused bool,
	subagent *schema.AgentInfo,
	derived schema.PermissionRuleset,
) (*schema.AgentInfo, schema.PermissionRuleset) {
	if !reused {
		return subagent, derived
	}
	existing, err := r.store.Get(childID)
	if err != nil || existing == nil {
		return subagent, derived
	}
	agent, rules := subagent, derived
	if existing.Agent != nil {
		if found := r.agents.Get(*existing.Agent); found != nil {
			agent = found
		}
	}
	if existing.Permission != nil {
		rules = *existing.Permission
	}
	return agent, rules
}

// publishTaskProgress reports the running task state on the parent session,
// mirroring the tool's progress metadata. The child session id links the
// subagent's events to this task.
func (r *runtime) publishTaskProgress(
	input TaskRunInput,
	childID schema.SessionID,
	subagent *schema.AgentInfo,
	parent *schema.SessionInfo,
) {
	if input.Progress == nil {
		return
	}
	_ = input.Progress(map[string]any{
		"sessionId":       string(childID),
		"parentSessionId": input.ParentSessionID,
		"subagentType":    input.SubagentType,
		"description":     input.Description,
		"prompt":          input.Prompt,
		"model":           childModelJSON(subagent, parent),
	}, nil)
}

// runChildSession admits the prompt and drains the child session's turn.
func (r *runtime) runChildSession(ctx context.Context, childID schema.SessionID, prompt string) error {
	if err := r.admit(childID, "", schema.Prompt{Text: prompt}, schema.SessionDeliveryQueue); err != nil {
		return err
	}
	runErr := r.coord.Run(ctx, childID)
	if ctx.Err() != nil {
		r.coord.Interrupt(childID)
	}
	return runErr
}

// checkSubagentDepth fails when nesting a subagent under parent would exceed
// the configured limit.
func (r *runtime) checkSubagentDepth(parent *schema.SessionInfo) error {
	depth, err := r.parentDepth(parent)
	if err != nil {
		return err
	}
	if depth >= r.subagentDepth {
		return fmt.Errorf(
			"Subagent depth limit reached (%d). Increase subagent depth to allow nested subagents",
			r.subagentDepth,
		)
	}
	return nil
}

// parentDepth counts the ancestors of session, stopping once the subagent limit
// is reached. Bounding the walk keeps a corrupt or cyclic parent chain from
// looping forever and only reads the rows the comparison needs.
func (r *runtime) parentDepth(session *schema.SessionInfo) (int, error) {
	depth := 0
	current := session
	for depth < r.subagentDepth && current.ParentID != nil {
		depth++
		next, err := r.store.Get(*current.ParentID)
		if err != nil {
			return 0, err
		}
		if next == nil {
			break
		}
		current = next
	}
	return depth, nil
}

// resolveChildSession reuses the session named by task_id, or inserts a new
// child session carrying parent_id and the derived permission ruleset.
func (r *runtime) resolveChildSession(
	input TaskRunInput,
	parent *schema.SessionInfo,
	subagent *schema.AgentInfo,
	derived schema.PermissionRuleset,
) (schema.SessionID, bool, error) {
	if input.TaskID != nil && *input.TaskID != "" {
		existing, err := r.store.Get(schema.SessionID(*input.TaskID))
		if err != nil {
			return "", false, err
		}
		if existing != nil {
			return existing.ID, true, nil
		}
	}
	childID := schema.NewSessionID()
	title := input.Description + " (@" + input.SubagentType + " subagent)"
	permissionJSON, err := encodePermissionRules(derived)
	if err != nil {
		return "", false, err
	}
	if err := insertSessionRow(r.db.DB, sessionRow{
		ID:         childID,
		ProjectID:  r.projectID,
		ParentID:   &input.ParentSessionID,
		Directory:  string(r.location.Directory),
		Title:      title,
		Agent:      input.SubagentType,
		Model:      childModelJSON(subagent, parent),
		Permission: permissionJSON,
	}); err != nil {
		return "", false, err
	}
	return childID, false, nil
}

func childModelJSON(subagent *schema.AgentInfo, parent *schema.SessionInfo) string {
	ref := subagent.Model
	if ref == nil {
		ref = parent.Model
	}
	if ref == nil {
		return "null"
	}
	return modelRefJSON(ref.ID, ref.ProviderID)
}

// lastAssistantText returns the last text part of the last assistant message.
func (r *runtime) lastAssistantText(sessionID schema.SessionID) (string, error) {
	messages, err := r.store.Context(sessionID)
	if err != nil {
		return "", err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		assistant, ok := messages[i].(schema.SessionMessageAssistant)
		if !ok {
			continue
		}
		for j := len(assistant.Content) - 1; j >= 0; j-- {
			if text, ok := assistant.Content[j].(schema.SessionMessageAssistantText); ok {
				return text.Text, nil
			}
		}
	}
	return "", nil
}
