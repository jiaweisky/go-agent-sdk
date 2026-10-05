package tool

import (
	"context"
	"fmt"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/core/util"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

type ExecuteInput struct {
	SessionID          string
	Agent              string
	AssistantMessageID string
	Call               schema.ToolCallPart
	Progress           func(structured map[string]any, content []Content) error
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
	})
	if err != nil {
		if failure, ok := err.(*Failure); ok {
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
