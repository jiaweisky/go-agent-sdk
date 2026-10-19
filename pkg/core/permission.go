package core

import (
	"context"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type (
	PermissionID      = schema.PermissionID
	PermissionSource  = schema.PermissionSource
	PermissionRequest = schema.PermissionRequest
	PermissionReply   = schema.PermissionReply
	PermissionEffect  = schema.PermissionEffect
	PermissionRule    = schema.PermissionRule
	PermissionRuleset = schema.PermissionRuleset
)

const (
	PermissionReplyOnce   = schema.PermissionReplyOnce
	PermissionReplyAlways = schema.PermissionReplyAlways
	PermissionReplyReject = schema.PermissionReplyReject

	PermissionEffectAllow = schema.PermissionEffectAllow
	PermissionEffectDeny  = schema.PermissionEffectDeny
	PermissionEffectAsk   = schema.PermissionEffectAsk

	PermissionSourceTypeTool = schema.PermissionSourceTypeTool
)

func NewPermissionID() PermissionID {
	return schema.NewPermissionID()
}

var missingAgentPermissions = PermissionRuleset{{Action: "*", Resource: "*", Effect: PermissionEffectDeny}}

type PermissionAssertInput struct {
	ID        *PermissionID
	SessionID string
	Action    string
	Resources []string
	Save      []string
	Metadata  map[string]any
	Source    *PermissionSource
	Agent     *string
}

type PermissionReplyInput struct {
	RequestID PermissionID
	Reply     PermissionReply
	Message   *string
}

type PermissionAskResult struct {
	ID     PermissionID
	Effect PermissionEffect
}

type PermissionDeclinedError struct{}

func (e *PermissionDeclinedError) Error() string {
	return "permission declined"
}

type PermissionCorrectedError struct {
	Feedback string
}

func (e *PermissionCorrectedError) Error() string {
	return "permission corrected: " + e.Feedback
}

type PermissionBlockedError struct {
	Rules PermissionRuleset
}

func (e *PermissionBlockedError) Error() string {
	for _, rule := range e.Rules {
		if rule.Effect == PermissionEffectDeny {
			return fmt.Sprintf("permission blocked: %s %q is denied by the current policy", rule.Action, rule.Resource)
		}
	}
	return "permission blocked: the current policy denies this action"
}

type PermissionNotFoundError struct {
	RequestID PermissionID
}

func (e *PermissionNotFoundError) Error() string {
	return fmt.Sprintf("permission request not found: %s", e.RequestID)
}

type PermissionDuplicateError struct {
	RequestID PermissionID
}

func (e *PermissionDuplicateError) Error() string {
	return fmt.Sprintf("duplicate pending permission request: %s", e.RequestID)
}

type PermissionSessionNotFoundError struct {
	SessionID string
}

func (e *PermissionSessionNotFoundError) Error() string {
	return fmt.Sprintf("session not found: %s", e.SessionID)
}

func PermissionEvaluate(action, resource string, rulesets ...PermissionRuleset) PermissionRule {
	var last *PermissionRule
	for _, ruleset := range rulesets {
		for i := range ruleset {
			rule := &ruleset[i]
			if util.Match(action, rule.Action) && util.Match(resource, rule.Resource) {
				last = rule
			}
		}
	}
	if last == nil {
		return PermissionRule{Action: action, Resource: "*", Effect: PermissionEffectAsk}
	}
	return *last
}

func PermissionMerge(rulesets ...PermissionRuleset) PermissionRuleset {
	var result PermissionRuleset
	for _, r := range rulesets {
		result = append(result, r...)
	}
	return result
}

func permissionDenied(action string, resources []string, rules PermissionRuleset) bool {
	for _, resource := range resources {
		if PermissionEvaluate(action, resource, rules).Effect == PermissionEffectDeny {
			return true
		}
	}
	return false
}

func permissionRelevant(action string, rules PermissionRuleset) PermissionRuleset {
	var result PermissionRuleset
	for _, rule := range rules {
		if util.Match(action, rule.Action) {
			result = append(result, rule)
		}
	}
	return result
}

type PermissionSessionInfo struct {
	ID    string
	Agent string
	// Permissions is an optional session-level ruleset merged after the
	// agent's permissions (used for task-tool subagent sessions).
	Permissions PermissionRuleset
}

type PermissionAgentInfo struct {
	ID          string
	Permissions PermissionRuleset
}

type PermissionSaved struct {
	ID        string
	ProjectID string
	Action    string
	Resource  string
}

type PermissionSessionStore interface {
	Get(sessionID string) (*PermissionSessionInfo, bool, error)
}

type PermissionAgentResolver interface {
	Resolve(agentID string) (*PermissionAgentInfo, bool)
}

type PermissionSavedStore interface {
	List(projectID string) ([]PermissionSaved, error)
	Add(projectID, action string, resources []string) error
}

type PermissionEventPublisher interface {
	PublishAsked(request PermissionRequest) error
	PublishReplied(sessionID string, requestID PermissionID, reply PermissionReply) error
}

type PermissionConfig struct {
	Sessions  PermissionSessionStore
	Agents    PermissionAgentResolver
	Saved     PermissionSavedStore
	Events    PermissionEventPublisher
	ProjectID string
}

type Permission interface {
	Ask(input PermissionAssertInput) (*PermissionAskResult, error)
	Assert(input PermissionAssertInput) error
	Reply(input PermissionReplyInput) error
	Get(id PermissionID) *PermissionRequest
	ForSession(sessionID string) []PermissionRequest
	List() []PermissionRequest
	Close() error
}

// permissionPending is an in-flight permission request; its meta is the agent
// the request was evaluated against.
type permissionPending = pendingEntry[PermissionID, PermissionRequest, error, *string]

type permissionService struct {
	cfg     PermissionConfig
	pending *pendingRegistry[PermissionID, PermissionRequest, error, *string]
}

func NewPermission(cfg PermissionConfig) Permission {
	return &permissionService{
		cfg:     cfg,
		pending: newPendingRegistry[PermissionID, PermissionRequest, error, *string](),
	}
}

// NewPermissionContext creates a Permission service bound to ctx. A background
// goroutine calls Close when ctx is cancelled, declining every pending request
// with PermissionDeclinedError. Callers must cancel ctx (e.g. defer cancel()) or
// the goroutine leaks.
func NewPermissionContext(ctx context.Context, cfg PermissionConfig) Permission {
	p := NewPermission(cfg)
	go func() {
		<-ctx.Done()
		_ = p.Close()
	}()
	return p
}

type permissionEvalResult struct {
	Effect PermissionEffect
	Rules  PermissionRuleset
}

func (s *permissionService) configured(sessionID string, agentID *string) (PermissionRuleset, error) {
	session, ok, err := s.cfg.Sessions.Get(sessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &PermissionSessionNotFoundError{SessionID: sessionID}
	}
	id := session.Agent
	if agentID != nil {
		id = *agentID
	}
	agent, ok := s.cfg.Agents.Resolve(id)
	if !ok {
		return PermissionMerge(missingAgentPermissions, session.Permissions), nil
	}
	return PermissionMerge(agent.Permissions, session.Permissions), nil
}

func (s *permissionService) savedRules() (PermissionRuleset, error) {
	items, err := s.cfg.Saved.List(s.cfg.ProjectID)
	if err != nil {
		return nil, err
	}
	rules := make(PermissionRuleset, len(items))
	for i, item := range items {
		rules[i] = PermissionRule{Action: item.Action, Resource: item.Resource, Effect: PermissionEffectAllow}
	}
	return rules, nil
}

func (s *permissionService) evaluateInput(input PermissionAssertInput) (*permissionEvalResult, error) {
	rules, err := s.configured(input.SessionID, input.Agent)
	if err != nil {
		return nil, err
	}
	if permissionDenied(input.Action, input.Resources, rules) {
		return &permissionEvalResult{Effect: PermissionEffectDeny, Rules: rules}, nil
	}
	saved, err := s.savedRules()
	if err != nil {
		return nil, err
	}
	all := PermissionMerge(rules, saved)
	effect := PermissionEffectAllow
	for _, resource := range input.Resources {
		switch PermissionEvaluate(input.Action, resource, all).Effect {
		case PermissionEffectDeny:
			effect = PermissionEffectDeny
		case PermissionEffectAsk:
			if effect != PermissionEffectDeny {
				effect = PermissionEffectAsk
			}
		}
	}
	return &permissionEvalResult{Effect: effect, Rules: all}, nil
}

func permissionRequestFrom(input PermissionAssertInput) PermissionRequest {
	var id PermissionID
	if input.ID != nil {
		id = *input.ID
	} else {
		id = NewPermissionID()
	}
	return PermissionRequest{
		ID:        id,
		SessionID: input.SessionID,
		Action:    input.Action,
		Resources: input.Resources,
		Save:      input.Save,
		Metadata:  input.Metadata,
		Source:    input.Source,
	}
}

func (s *permissionService) create(req PermissionRequest, agent *string) (*permissionPending, error) {
	item, closed, duplicate := s.pending.add(req.ID, req, agent)
	if closed {
		return nil, &PermissionDeclinedError{}
	}
	if duplicate {
		return nil, &PermissionDuplicateError{RequestID: req.ID}
	}

	if err := s.cfg.Events.PublishAsked(req); err != nil {
		s.pending.finish(req.ID)
		return nil, err
	}
	return item, nil
}

func (s *permissionService) Ask(input PermissionAssertInput) (*PermissionAskResult, error) {
	result, err := s.evaluateInput(input)
	if err != nil {
		return nil, err
	}
	value := permissionRequestFrom(input)
	if result.Effect == PermissionEffectAsk {
		if _, err := s.create(value, input.Agent); err != nil {
			return nil, err
		}
	}
	return &PermissionAskResult{ID: value.ID, Effect: result.Effect}, nil
}

func (s *permissionService) Assert(input PermissionAssertInput) error {
	result, err := s.evaluateInput(input)
	if err != nil {
		return err
	}
	if result.Effect == PermissionEffectDeny {
		return &PermissionBlockedError{Rules: permissionRelevant(input.Action, result.Rules)}
	}
	if result.Effect == PermissionEffectAllow {
		return nil
	}
	item, err := s.create(permissionRequestFrom(input), input.Agent)
	if err != nil {
		return err
	}
	err = <-item.done
	s.pending.finish(item.request.ID)
	return err
}

func (s *permissionService) Reply(input PermissionReplyInput) error {
	existing, err := s.reserve(input.RequestID)
	if err != nil {
		return err
	}

	if err := s.cfg.Events.PublishReplied(existing.request.SessionID, existing.request.ID, input.Reply); err != nil {
		s.release(existing)
		return err
	}

	if input.Reply == PermissionReplyReject {
		return s.rejectReply(existing, input)
	}

	if input.Reply == PermissionReplyAlways && len(existing.request.Save) > 0 {
		if err := s.cfg.Saved.Add(s.cfg.ProjectID, existing.request.Action, existing.request.Save); err != nil {
			s.release(existing)
			return err
		}
	}
	existing.done <- nil
	s.pending.finish(input.RequestID)

	if input.Reply != PermissionReplyAlways || len(existing.request.Save) == 0 {
		return nil
	}
	return s.approveRemembered()
}

// rejectReply settles the replied request and declines every other pending
// request of the same session.
func (s *permissionService) rejectReply(existing *permissionPending, input PermissionReplyInput) error {
	var replyErr error = &PermissionDeclinedError{}
	if input.Message != nil {
		replyErr = &PermissionCorrectedError{Feedback: *input.Message}
	}
	existing.done <- replyErr
	s.pending.finish(existing.id)

	victims := s.pending.takeUnsettled(func(item *permissionPending) bool {
		return item.request.SessionID == existing.request.SessionID
	})
	for _, item := range victims {
		_ = s.cfg.Events.PublishReplied(item.request.SessionID, item.request.ID, PermissionReplyReject)
		item.done <- &PermissionDeclinedError{}
	}
	return nil
}

// approveRemembered settles every pending request that the newly saved rules
// now allow.
func (s *permissionService) approveRemembered() error {
	rememberedRules, err := s.savedRules()
	if err != nil {
		return err
	}

	approved := s.pending.takeUnsettled(func(item *permissionPending) bool {
		return s.rememberedAllows(item, rememberedRules)
	})
	for _, item := range approved {
		_ = s.cfg.Events.PublishReplied(item.request.SessionID, item.request.ID, PermissionReplyAlways)
		item.done <- nil
	}
	return nil
}

// rememberedAllows reports whether the remembered rules allow every resource of
// a pending request that configuration does not already deny.
func (s *permissionService) rememberedAllows(item *permissionPending, remembered PermissionRuleset) bool {
	rules, err := s.configured(item.request.SessionID, item.meta)
	if err != nil {
		return false
	}
	if permissionDenied(item.request.Action, item.request.Resources, rules) {
		return false
	}
	effective := PermissionMerge(rules, remembered)
	for _, resource := range item.request.Resources {
		if PermissionEvaluate(item.request.Action, resource, effective).Effect != PermissionEffectAllow {
			return false
		}
	}
	return true
}

func (s *permissionService) reserve(id PermissionID) (*permissionPending, error) {
	existing, ok := s.pending.reserve(id)
	if !ok {
		return nil, &PermissionNotFoundError{RequestID: id}
	}
	return existing, nil
}

func (s *permissionService) release(existing *permissionPending) {
	if s.pending.release(existing) {
		existing.done <- &PermissionDeclinedError{}
	}
}

// Close declines every pending request with PermissionDeclinedError without
// publishing an event. It is idempotent.
func (s *permissionService) Close() error {
	for _, item := range s.pending.close() {
		item.done <- &PermissionDeclinedError{}
	}
	return nil
}

func (s *permissionService) List() []PermissionRequest {
	return s.pending.listWhere(func(*permissionPending) bool { return true })
}

func (s *permissionService) Get(id PermissionID) *PermissionRequest {
	req, ok := s.pending.get(id)
	if !ok {
		return nil
	}
	return &req
}

func (s *permissionService) ForSession(sessionID string) []PermissionRequest {
	return s.pending.listWhere(func(item *permissionPending) bool {
		return item.request.SessionID == sessionID
	})
}
