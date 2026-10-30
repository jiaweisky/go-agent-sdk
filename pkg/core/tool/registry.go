package tool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

type ExecuteInput struct {
	SessionID          string
	Agent              string
	AssistantMessageID string
	Call               schema.ToolCallPart
	Progress           func(structured map[string]any, content []Content) error
	ReportViolation    func(core.PolicyViolation)
}

type Settlement struct {
	Result      schema.ToolResultValue
	Output      *schema.ToolOutput
	OutputPaths []string
}

type registrationEntry struct {
	identity interface{}
	token    interface{}
	tool     Tool
}

type applicationEntry struct {
	identity *int
	tool     Tool
}

type OutputStore interface {
	Bound(sessionID string, toolCallID string, output schema.ToolOutput) BoundResult
}

type BoundResult struct {
	Output      schema.ToolOutput
	OutputPaths []string
}

type PermissionRule = core.PermissionRule

type Registry struct {
	mu           sync.Mutex
	local        map[string][]registrationEntry
	applications map[string]applicationEntry
	store        OutputStore
}

type RegistryOption func(*Registry)

func WithApplicationTools(tools map[string]Tool) RegistryOption {
	return func(r *Registry) {
		if r.applications == nil {
			r.applications = make(map[string]applicationEntry)
		}
		for k, v := range tools {
			r.applications[k] = applicationEntry{identity: new(int), tool: v}
		}
	}
}

func WithOutputStore(store OutputStore) RegistryOption {
	return func(r *Registry) {
		r.store = store
	}
}

func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		local:        make(map[string][]registrationEntry),
		applications: make(map[string]applicationEntry),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Registry) Register(tools map[string]Tool) (func(), error) {
	if len(tools) == 0 {
		return func() {}, nil
	}

	for name := range tools {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
	}

	token := new(int)
	r.mu.Lock()
	for name, tl := range tools {
		r.local[name] = append(r.local[name], registrationEntry{
			identity: new(int),
			token:    token,
			tool:     tl,
		})
	}
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for name := range tools {
			entries := r.local[name]
			var kept []registrationEntry
			for _, ent := range entries {
				if ent.token != token {
					kept = append(kept, ent)
				}
			}
			if len(kept) > 0 {
				r.local[name] = kept
			} else {
				delete(r.local, name)
			}
		}
	}, nil
}

type Materialization struct {
	Definitions []schema.ToolDefinition
	Settle      func(execCtx context.Context, input ExecuteInput) (*Settlement, error)
}

// MaterializeFor is the session-aware materialization seam. The core registry
// is session-agnostic, so it ignores the session id; embedders that scope tools
// per session (for example the high-level agent package) provide their own.
func (r *Registry) MaterializeFor(sessionID string, permissions []PermissionRule) (*Materialization, error) {
	return r.Materialize(permissions), nil
}

func (r *Registry) Materialize(permissions []PermissionRule) *Materialization {
	r.mu.Lock()

	registrations := make(map[string]registrationEntry)
	for name, app := range r.applications {
		registrations[name] = registrationEntry{
			identity: app.identity,
			tool:     app.tool,
		}
	}
	for name, entries := range r.local {
		if len(entries) > 0 {
			registrations[name] = entries[len(entries)-1]
		}
	}

	for name, entry := range registrations {
		if whollyDisabled(Permission(entry.tool, name), permissions) {
			delete(registrations, name)
		}
	}

	defs := make([]schema.ToolDefinition, 0, len(registrations))
	for name, entry := range registrations {
		defs = append(defs, Definition(name, entry.tool))
	}
	// Map iteration order is random; sort so the model-facing tool list is
	// stable across turns (providers cache on the request prefix).
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })

	identities := make(map[string]interface{}, len(registrations))
	for name, entry := range registrations {
		identities[name] = entry.identity
	}

	r.mu.Unlock()

	return &Materialization{
		Definitions: defs,
		Settle: func(execCtx context.Context, input ExecuteInput) (*Settlement, error) {
			return r.settle(execCtx, input, identities)
		},
	}
}

func (r *Registry) settle(
	execCtx context.Context,
	input ExecuteInput,
	identities map[string]interface{},
) (*Settlement, error) {
	identity, known := identities[input.Call.Name]
	if !known {
		return &Settlement{
			Result: schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Unknown tool: %s", input.Call.Name)},
		}, nil
	}

	current, ok := r.lookup(input.Call.Name)
	if !ok {
		return &Settlement{
			Result: schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Stale tool call: %s", input.Call.Name)},
		}, nil
	}

	if current.identity != identity {
		return &Settlement{
			Result: schema.ToolResultValue{Type: "error", Value: fmt.Sprintf("Stale tool call: %s", input.Call.Name)},
		}, nil
	}

	output, err := Settle(current.tool, execCtx, input.Call, Context{
		SessionID:          input.SessionID,
		Agent:              input.Agent,
		AssistantMessageID: input.AssistantMessageID,
		ToolCallID:         input.Call.ID,
		Progress:           input.Progress,
		ReportViolation:    input.ReportViolation,
	})
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) {
			if input.ReportViolation != nil {
				var denied *core.PolicyDeniedError
				if errors.As(failure.Cause, &denied) {
					input.ReportViolation(denied.Violation)
				}
			}
			return &Settlement{
				Result: schema.ToolResultValue{Type: "error", Value: failure.Message},
			}, nil
		}
		return nil, err
	}

	if r.store == nil {
		result := schema.ToolOutputToResultValue(output)
		s := &Settlement{Result: result}
		if result.Type != "error" {
			s.Output = &output
		}
		return s, nil
	}

	bounded := r.store.Bound(input.SessionID, input.Call.ID, output)
	result := schema.ToolOutputToResultValue(bounded.Output)

	s := &Settlement{Result: result, OutputPaths: bounded.OutputPaths}
	if result.Type != "error" {
		s.Output = &bounded.Output
	}
	return s, nil
}

// Restrict filters a materialization to a tool-name allowlist (the explicit
// counterpart of the deny-with-resource-"*" removal; see whollyDisabled): the
// model-facing definitions are reduced to the listed names, and the settle
// path rejects anything else, so a hallucinated call cannot reach a
// filtered-out tool. A nil list keeps every tool (the unset default); a
// non-nil empty list rejects every tool. Matching is by exact tool name.
func Restrict(mat *Materialization, names []string) *Materialization {
	if mat == nil || names == nil {
		return mat
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	defs := make([]schema.ToolDefinition, 0, len(mat.Definitions))
	for _, def := range mat.Definitions {
		if allowed[def.Name] {
			defs = append(defs, def)
		}
	}
	inner := mat.Settle
	return &Materialization{
		Definitions: defs,
		Settle: func(execCtx context.Context, input ExecuteInput) (*Settlement, error) {
			if !allowed[input.Call.Name] {
				return &Settlement{Result: schema.ToolResultValue{Type: "error", Value: "Unknown tool: " + input.Call.Name}}, nil
			}
			return inner(execCtx, input)
		},
	}
}

// Exclude removes the named tools from a materialization after an allowlist.
// Unlike Restrict, an empty/nil list removes nothing. Removed tools are absent
// from the model-facing definitions and their calls are rejected as unknown.
func Exclude(mat *Materialization, names []string) *Materialization {
	if mat == nil || len(names) == 0 {
		return mat
	}
	denied := make(map[string]bool, len(names))
	for _, name := range names {
		denied[name] = true
	}
	defs := make([]schema.ToolDefinition, 0, len(mat.Definitions))
	for _, def := range mat.Definitions {
		if !denied[def.Name] {
			defs = append(defs, def)
		}
	}
	inner := mat.Settle
	return &Materialization{
		Definitions: defs,
		Settle: func(execCtx context.Context, input ExecuteInput) (*Settlement, error) {
			if denied[input.Call.Name] {
				return &Settlement{Result: schema.ToolResultValue{Type: "error", Value: "Unknown tool: " + input.Call.Name}}, nil
			}
			return inner(execCtx, input)
		},
	}
}

func (r *Registry) lookup(name string) (registrationEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.local[name]
	if len(entries) > 0 {
		return entries[len(entries)-1], true
	}
	app, ok := r.applications[name]
	if ok {
		return registrationEntry{identity: app.identity, tool: app.tool}, true
	}
	return registrationEntry{}, false
}

// whollyDisabled reports whether a tool's action is denied for all resources:
// the last matching rule being deny-with-resource-"*" means the tool is
// dropped from the materialization (not listed to the model, and its settle
// lookup reports an unknown tool) rather than merely rejected at call time.
// This is how a permissions deny rule doubles as tool removal; the explicit
// way to scope an agent's toolset is AgentInfo.Tools (an allowlist).
func whollyDisabled(action string, rules []PermissionRule) bool {
	var lastMatch *PermissionRule
	for i := range rules {
		if util.Match(action, rules[i].Action) {
			lastMatch = &rules[i]
		}
	}
	if lastMatch == nil {
		return false
	}
	return lastMatch.Resource == "*" && lastMatch.Effect == "deny"
}
