package agent

import (
	"sync"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/schema"
)

// PermissionRequest describes an approval request raised before a tool runs.
type PermissionRequest struct {
	SessionID string
	Agent     string
	Action    string
	Resources []string
	Metadata  map[string]any
}

// PermissionDecision is the handler's answer.
type PermissionDecision struct {
	// Allow approves the request for this invocation.
	Allow bool
	// Message is optional feedback returned to the model on denial.
	Message string
}

// PermissionHandlerFunc approves or denies a permission request.
type PermissionHandlerFunc func(request PermissionRequest) (PermissionDecision, error)

// ApproveAll approves every permission request.
var ApproveAll PermissionHandlerFunc = func(PermissionRequest) (PermissionDecision, error) {
	return PermissionDecision{Allow: true}, nil
}

// handlerPermission decorates a core Permission service so that "ask" decisions
// are answered by the configured PermissionHandlerFunc instead of blocking for
// an external reply.
type handlerPermission struct {
	inner   core.Permission
	handler PermissionHandlerFunc
}

func (p handlerPermission) Ask(input core.PermissionAssertInput) (*core.PermissionAskResult, error) {
	return p.inner.Ask(input)
}

func (p handlerPermission) Assert(input core.PermissionAssertInput) error {
	result, err := p.inner.Ask(input)
	if err != nil {
		return err
	}
	switch result.Effect {
	case core.PermissionEffectAllow:
		return nil
	case core.PermissionEffectDeny:
		return &core.PermissionDeclinedError{}
	}
	if p.handler == nil {
		return &core.PermissionDeclinedError{}
	}
	agent := ""
	if input.Agent != nil {
		agent = *input.Agent
	}
	decision, err := p.handler(PermissionRequest{
		SessionID: input.SessionID,
		Agent:     agent,
		Action:    input.Action,
		Resources: input.Resources,
		Metadata:  input.Metadata,
	})
	if err != nil {
		return err
	}
	if decision.Allow {
		_ = p.inner.Reply(core.PermissionReplyInput{RequestID: result.ID, Reply: core.PermissionReplyOnce})
		return nil
	}
	reply := core.PermissionReplyInput{RequestID: result.ID, Reply: core.PermissionReplyReject}
	if decision.Message != "" {
		message := decision.Message
		reply.Message = &message
	}
	_ = p.inner.Reply(reply)
	return &core.PermissionDeclinedError{}
}

func (p handlerPermission) Reply(input core.PermissionReplyInput) error {
	return p.inner.Reply(input)
}

func (p handlerPermission) Get(id core.PermissionID) *core.PermissionRequest {
	return p.inner.Get(id)
}

func (p handlerPermission) ForSession(sessionID string) []core.PermissionRequest {
	return p.inner.ForSession(sessionID)
}

func (p handlerPermission) List() []core.PermissionRequest {
	return p.inner.List()
}

func (p handlerPermission) Close() error {
	return p.inner.Close()
}

// permissionSessions is an in-memory PermissionSessionStore.
type permissionSessions struct {
	mu     sync.RWMutex
	agents map[string]string
}

func newPermissionSessions() *permissionSessions {
	return &permissionSessions{agents: map[string]string{}}
}

func (s *permissionSessions) Set(sessionID, agent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[sessionID] = agent
}

func (s *permissionSessions) Get(sessionID string) (*core.PermissionSessionInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.agents[sessionID]
	if !ok {
		return nil, false
	}
	return &core.PermissionSessionInfo{ID: sessionID, Agent: agent}, true
}

// permissionAgents adapts core.Agent to PermissionAgentResolver.
type permissionAgents struct{ agents *core.Agent }

func (a permissionAgents) Resolve(agentID string) (*core.PermissionAgentInfo, bool) {
	info := a.agents.Get(schema.AgentID(agentID))
	if info == nil {
		return nil, false
	}
	return &core.PermissionAgentInfo{ID: string(info.ID), Permissions: info.Permissions}, true
}

// permissionSaved is an in-memory PermissionSavedStore.
type permissionSaved struct {
	mu    sync.Mutex
	rules []core.PermissionSaved
}

func (s *permissionSaved) List(projectID string) ([]core.PermissionSaved, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []core.PermissionSaved
	for _, rule := range s.rules {
		if rule.ProjectID == projectID {
			result = append(result, rule)
		}
	}
	return result, nil
}

func (s *permissionSaved) Add(projectID, action string, resources []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, resource := range resources {
		s.rules = append(s.rules, core.PermissionSaved{
			ID:        string(schema.NewPermissionID()),
			ProjectID: projectID,
			Action:    action,
			Resource:  resource,
		})
	}
	return nil
}

// permissionEvents publishes permission lifecycle events.
type permissionEvents struct{ events core.Event }

func (p permissionEvents) PublishAsked(request core.PermissionRequest) error {
	_, err := p.events.Publish(schema.PermissionEventAsked, map[string]any{
		"id":        string(request.ID),
		"sessionID": request.SessionID,
		"action":    request.Action,
		"resources": request.Resources,
		"save":      request.Save,
		"metadata":  request.Metadata,
		"source":    request.Source,
	}, nil)
	return err
}

func (p permissionEvents) PublishReplied(sessionID string, requestID core.PermissionID, reply core.PermissionReply) error {
	_, err := p.events.Publish(schema.PermissionEventReplied, map[string]any{
		"sessionID": sessionID,
		"requestID": string(requestID),
		"reply":     string(reply),
	}, nil)
	return err
}
