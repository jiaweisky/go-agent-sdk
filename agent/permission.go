package agent

import (
	"sync"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/schema"
)

// PermissionRequest describes an approval request raised before a tool runs.
type PermissionRequest struct {
	SessionID string
	Agent     string
	Action    string
	Resources []string
	// Save lists the resources the tool offers to remember if the handler
	// approves with Always. Empty when the request cannot be remembered.
	Save     []string
	Metadata map[string]any
}

// PermissionDecision is the handler's answer.
type PermissionDecision struct {
	// Allow approves the request for this invocation.
	Allow bool
	// Always also remembers the approval (saving the request's Save rules) so
	// the same action is allowed without asking again. Ignored when Allow is
	// false or the request has no Save rules.
	Always bool
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
// are answered by a PermissionHandlerFunc instead of blocking for an external
// reply. A per-session handler overrides the client default.
type handlerPermission struct {
	inner core.Permission

	mu       sync.Mutex
	def      PermissionHandlerFunc
	handlers map[string]PermissionHandlerFunc
}

func (p *handlerPermission) setHandler(sessionID string, handler PermissionHandlerFunc) {
	if handler == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handlers == nil {
		p.handlers = map[string]PermissionHandlerFunc{}
	}
	p.handlers[sessionID] = handler
}

func (p *handlerPermission) removeHandler(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.handlers, sessionID)
}

func (p *handlerPermission) handlerFor(sessionID string) PermissionHandlerFunc {
	p.mu.Lock()
	defer p.mu.Unlock()
	if handler, ok := p.handlers[sessionID]; ok && handler != nil {
		return handler
	}
	if p.def != nil {
		return p.def
	}
	return ApproveAll
}

func (p *handlerPermission) Ask(input core.PermissionAssertInput) (*core.PermissionAskResult, error) {
	return p.inner.Ask(input)
}

func (p *handlerPermission) Assert(input core.PermissionAssertInput) error {
	result, err := p.inner.Ask(input)
	if err != nil {
		return err
	}
	switch result.Effect {
	case core.PermissionEffectAllow:
		return nil
	case core.PermissionEffectDeny:
		// A rule-based deny is a blocked tool, not a user decline: it must not
		// halt the turn (mirrors TS BlockedError vs DeclinedError).
		return &core.PermissionBlockedError{}
	}
	agent := ""
	if input.Agent != nil {
		agent = *input.Agent
	}
	decision, err := p.handlerFor(input.SessionID)(PermissionRequest{
		SessionID: input.SessionID,
		Agent:     agent,
		Action:    input.Action,
		Resources: input.Resources,
		Save:      input.Save,
		Metadata:  input.Metadata,
	})
	if err != nil {
		return err
	}
	if decision.Allow {
		reply := core.PermissionReplyOnce
		if decision.Always && len(input.Save) > 0 {
			reply = core.PermissionReplyAlways
		}
		_ = p.inner.Reply(core.PermissionReplyInput{RequestID: result.ID, Reply: reply})
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

func (p *handlerPermission) Reply(input core.PermissionReplyInput) error {
	return p.inner.Reply(input)
}

func (p *handlerPermission) Get(id core.PermissionID) *core.PermissionRequest {
	return p.inner.Get(id)
}

func (p *handlerPermission) ForSession(sessionID string) []core.PermissionRequest {
	return p.inner.ForSession(sessionID)
}

func (p *handlerPermission) List() []core.PermissionRequest {
	return p.inner.List()
}

func (p *handlerPermission) Close() error {
	return p.inner.Close()
}

// permissionSessionStore adapts the session store so permission asserts and
// tool materialization read the same session row (a single source of truth).
type permissionSessionStore struct {
	store *session.Store
}

func (s permissionSessionStore) Get(sessionID string) (*core.PermissionSessionInfo, bool, error) {
	info, err := s.store.Get(schema.SessionID(sessionID))
	if err != nil {
		return nil, false, err
	}
	if info == nil {
		return nil, false, nil
	}
	agent := ""
	if info.Agent != nil {
		agent = string(*info.Agent)
	}
	var permissions schema.PermissionRuleset
	if info.Permission != nil {
		permissions = *info.Permission
	}
	return &core.PermissionSessionInfo{
		ID:          sessionID,
		Agent:       agent,
		Permissions: permissions,
	}, true, nil
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

// permissionSaved persists "always allow" rules so they survive restarts
// (mirrors the TS PermissionSaved table).
type permissionSaved struct{ db *database.Database }

func (s *permissionSaved) List(projectID string) ([]core.PermissionSaved, error) {
	rows, err := s.db.DB.Query(
		"SELECT `id`,`project_id`,`action`,`resource` FROM `permission` WHERE `project_id` = ?",
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []core.PermissionSaved
	for rows.Next() {
		var item core.PermissionSaved
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Action, &item.Resource); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *permissionSaved) Add(projectID, action string, resources []string) error {
	if len(resources) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	for _, resource := range resources {
		if _, err := s.db.DB.Exec(
			"INSERT INTO `permission` (`id`,`project_id`,`action`,`resource`,`time_created`,`time_updated`) "+
				"VALUES (?,?,?,?,?,?) ON CONFLICT(`project_id`,`action`,`resource`) DO NOTHING",
			string(schema.NewPermissionID()), projectID, action, resource, now, now,
		); err != nil {
			return err
		}
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
