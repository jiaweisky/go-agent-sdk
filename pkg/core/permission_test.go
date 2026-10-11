package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPermissionEvaluateLastMatchWins guards that a specific rule after a
// wildcard rule overrides it, so "allow *" + "deny question" denies question
// while allowing everything else.
func TestPermissionEvaluateLastMatchWins(t *testing.T) {
	rules := PermissionRuleset{
		{Action: "*", Resource: "*", Effect: PermissionEffectAllow},
		{Action: "question", Resource: "*", Effect: PermissionEffectDeny},
	}
	if got := PermissionEvaluate("question", "*", rules); got.Effect != PermissionEffectDeny {
		t.Fatalf("question effect = %q, want deny", got.Effect)
	}
	if got := PermissionEvaluate("bash", "*", rules); got.Effect != PermissionEffectAllow {
		t.Fatalf("bash effect = %q, want allow", got.Effect)
	}
}

type permTestSessions struct{ agent string }

func (s permTestSessions) Get(sessionID string) (*PermissionSessionInfo, bool, error) {
	return &PermissionSessionInfo{ID: sessionID, Agent: s.agent}, true, nil
}

type permTestAgents struct{ rules PermissionRuleset }

func (a permTestAgents) Resolve(agentID string) (*PermissionAgentInfo, bool) {
	return &PermissionAgentInfo{ID: agentID, Permissions: a.rules}, true
}

type permTestSaved struct{}

func (permTestSaved) List(projectID string) ([]PermissionSaved, error)       { return nil, nil }
func (permTestSaved) Add(projectID, action string, resources []string) error { return nil }

type permTestEvents struct {
	asked   int
	replied int
}

func (e *permTestEvents) PublishAsked(request PermissionRequest) error { e.asked++; return nil }
func (e *permTestEvents) PublishReplied(sessionID string, requestID PermissionID, reply PermissionReply) error {
	e.replied++
	return nil
}

func permTestConfig(events *permTestEvents) PermissionConfig {
	return PermissionConfig{
		Sessions: permTestSessions{agent: "build"},
		Agents: permTestAgents{rules: PermissionRuleset{
			{Action: "*", Resource: "*", Effect: PermissionEffectAsk},
		}},
		Saved:     permTestSaved{},
		Events:    events,
		ProjectID: "proj_1",
	}
}

func newPermTestService(events *permTestEvents) Permission {
	return NewPermission(permTestConfig(events))
}

func permTestAssertInput() PermissionAssertInput {
	return PermissionAssertInput{
		SessionID: "ses_1",
		Action:    "edit",
		Resources: []string{"*"},
	}
}

func waitForPermissionPending(t *testing.T, p Permission) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(p.List()) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for pending permission")
}

func TestPermission_CloseDeclinesPending(t *testing.T) {
	events := &permTestEvents{}
	p := newPermTestService(events)

	errs := make(chan error, 1)
	go func() { errs <- p.Assert(permTestAssertInput()) }()
	waitForPermissionPending(t, p)

	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-errs:
		var declined *PermissionDeclinedError
		if !errors.As(err, &declined) {
			t.Fatalf("expected declined error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for close to decline pending assert")
	}

	if list := p.List(); len(list) != 0 {
		t.Fatalf("expected no pending after close, got %d", len(list))
	}
	if events.asked != 1 {
		t.Fatalf("expected 1 asked event, got %d", events.asked)
	}
	if events.replied != 0 {
		t.Fatalf("close must not publish replied events, got %d", events.replied)
	}
}

func TestPermission_AssertAfterClose(t *testing.T) {
	p := newPermTestService(&permTestEvents{})
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	err := p.Assert(permTestAssertInput())
	var declined *PermissionDeclinedError
	if !errors.As(err, &declined) {
		t.Fatalf("expected declined error, got %v", err)
	}
}

func TestPermission_CloseWithoutPending(t *testing.T) {
	p := newPermTestService(&permTestEvents{})
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestPermission_ContextCancelDeclinesPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPermissionContext(ctx, permTestConfig(&permTestEvents{}))

	errs := make(chan error, 1)
	go func() { errs <- p.Assert(permTestAssertInput()) }()
	waitForPermissionPending(t, p)

	cancel()

	select {
	case err := <-errs:
		var declined *PermissionDeclinedError
		if !errors.As(err, &declined) {
			t.Fatalf("expected declined error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context cancel to decline pending assert")
	}
	if list := p.List(); len(list) != 0 {
		t.Fatalf("expected no pending after cancel, got %d", len(list))
	}
}

func TestPermission_ReplyApproves(t *testing.T) {
	events := &permTestEvents{}
	p := newPermTestService(events)

	errs := make(chan error, 1)
	go func() { errs <- p.Assert(permTestAssertInput()) }()
	waitForPermissionPending(t, p)

	pending := p.List()[0]
	if err := p.Reply(PermissionReplyInput{RequestID: pending.ID, Reply: PermissionReplyOnce}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("expected approval, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval")
	}
	if list := p.List(); len(list) != 0 {
		t.Fatalf("expected no pending after reply, got %d", len(list))
	}
}

type permTestErrorSessions struct{ err error }

func (s permTestErrorSessions) Get(sessionID string) (*PermissionSessionInfo, bool, error) {
	return nil, false, s.err
}

func TestPermission_AssertPropagatesSessionStoreError(t *testing.T) {
	want := errors.New("session store unavailable")
	cfg := permTestConfig(&permTestEvents{})
	cfg.Sessions = permTestErrorSessions{err: want}

	err := NewPermission(cfg).Assert(permTestAssertInput())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
