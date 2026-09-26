package agent

import (
	"encoding/json"
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

// sessionHandlers tracks per-session handler overrides over a default, so a
// session can replace the client-wide handler. It is shared by the permission
// and question decorators.
type sessionHandlers[H any] struct {
	mu       sync.Mutex
	def      H
	handlers map[string]H
}

func newSessionHandlers[H any](def H) *sessionHandlers[H] {
	return &sessionHandlers[H]{def: def, handlers: map[string]H{}}
}

// set records a per-session handler.
func (h *sessionHandlers[H]) set(sessionID string, handler H) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[sessionID] = handler
}

func (h *sessionHandlers[H]) remove(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.handlers, sessionID)
}

// resolve returns the session's handler, falling back to the default. The
// default is normalized to non-nil by the concrete constructors, so it is
// always safe to return.
func (h *sessionHandlers[H]) resolve(sessionID string) H {
	h.mu.Lock()
	defer h.mu.Unlock()
	if handler, ok := h.handlers[sessionID]; ok {
		return handler
	}
	return h.def
}

// permissionHandler decorates a core Permission service so that "ask" decisions
// are answered by a PermissionHandlerFunc instead of blocking for an external
// reply. A per-session handler overrides the client default.
type permissionHandler struct {
	inner    core.Permission
	handlers *sessionHandlers[PermissionHandlerFunc]
}

func newPermissionHandler(inner core.Permission, def PermissionHandlerFunc) *permissionHandler {
	if def == nil {
		def = ApproveAll
	}
	return &permissionHandler{inner: inner, handlers: newSessionHandlers(def)}
}

func (p *permissionHandler) setHandler(sessionID string, handler PermissionHandlerFunc) {
	if handler == nil {
		return
	}
	p.handlers.set(sessionID, handler)
}

func (p *permissionHandler) removeHandler(sessionID string) {
	p.handlers.remove(sessionID)
}

// handlerFor returns the session's effective handler: its override when set,
// otherwise the client default.
func (p *permissionHandler) handlerFor(sessionID string) PermissionHandlerFunc {
	return p.handlers.resolve(sessionID)
}

func (p *permissionHandler) Ask(input core.PermissionAssertInput) (*core.PermissionAskResult, error) {
	return p.inner.Ask(input)
}

func (p *permissionHandler) Assert(input core.PermissionAssertInput) error {
	result, err := p.inner.Ask(input)
	if err != nil {
		return err
	}
	switch result.Effect {
	case core.PermissionEffectAllow:
		return nil
	case core.PermissionEffectDeny:
		// A rule-based deny is a blocked tool, not a user decline: it must not
		// halt the turn, unlike a user decline.
		return &core.PermissionBlockedError{}
	}
	decision, err := p.handlers.resolve(input.SessionID)(newPermissionRequest(input))
	if err != nil {
		return err
	}
	return p.applyDecision(result.ID, input.Save, decision)
}

// applyDecision records the handler's decision and returns the error matching
// it.
func (p *permissionHandler) applyDecision(requestID core.PermissionID, save []string, decision PermissionDecision) error {
	if decision.Allow {
		reply := core.PermissionReplyOnce
		if decision.Always && len(save) > 0 {
			reply = core.PermissionReplyAlways
		}
		_ = p.inner.Reply(core.PermissionReplyInput{RequestID: requestID, Reply: reply})
		return nil
	}
	reply := core.PermissionReplyInput{RequestID: requestID, Reply: core.PermissionReplyReject}
	if decision.Message != "" {
		message := decision.Message
		reply.Message = &message
	}
	_ = p.inner.Reply(reply)
	return &core.PermissionDeclinedError{}
}

// newPermissionRequest flattens a core assert input into the handler request.
func newPermissionRequest(input core.PermissionAssertInput) PermissionRequest {
	agent := ""
	if input.Agent != nil {
		agent = *input.Agent
	}
	return PermissionRequest{
		SessionID: input.SessionID,
		Agent:     agent,
		Action:    input.Action,
		Resources: input.Resources,
		Save:      input.Save,
		Metadata:  input.Metadata,
	}
}

func (p *permissionHandler) Reply(input core.PermissionReplyInput) error {
	return p.inner.Reply(input)
}

func (p *permissionHandler) Get(id core.PermissionID) *core.PermissionRequest {
	return p.inner.Get(id)
}

func (p *permissionHandler) ForSession(sessionID string) []core.PermissionRequest {
	return p.inner.ForSession(sessionID)
}

func (p *permissionHandler) List() []core.PermissionRequest {
	return p.inner.List()
}

func (p *permissionHandler) Close() error {
	return p.inner.Close()
}

// permissionSessionStore adapts the session store for permission asserts, so
// agent and session rules are read from the durable session row.
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
	return &core.PermissionSessionInfo{
		ID:          sessionID,
		Agent:       agentIDOf(info),
		Permissions: permissionRulesOf(info),
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

// permissionSaved persists "always allow" rules so they survive restarts.
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

// encodePermissionRules renders a ruleset as the JSON stored in a session row.
// An empty ruleset encodes as JSON null.
func encodePermissionRules(rules schema.PermissionRuleset) (string, error) {
	if len(rules) == 0 {
		return "null", nil
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
