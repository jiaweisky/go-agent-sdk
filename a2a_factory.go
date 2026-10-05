package agent

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/a2a/server"
	coresession "github.com/jiaweisky/go-agent-sdk/core/session"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// TurnStore returns a task store over the client's database, so an A2A server
// persists tasks durably (and survives restarts). Pass it to
// server.Options.Turns.
func (c *Client) TurnStore() *coresession.TurnStore {
	return coresession.NewTurnStore(c.rt.db)
}

// a2aFactory adapts a Client to the A2A server's session surface. It binds the
// inbound permissions every externally triggered session runs under.
type a2aFactory struct {
	client      *Client
	permissions schema.PermissionRuleset
}

// A2AInboundConfig configures how an A2A server drives sessions.
type A2AInboundConfig struct {
	// Permissions is the ruleset applied to sessions created by inbound A2A
	// calls.
	Permissions schema.PermissionRuleset
}

// A2AFactoryOption customizes NewA2AFactory.
type A2AFactoryOption func(*A2AInboundConfig)

// WithInboundPermissions overrides the ruleset applied to externally triggered
// sessions.
func WithInboundPermissions(rules schema.PermissionRuleset) A2AFactoryOption {
	return func(config *A2AInboundConfig) { config.Permissions = rules }
}

// DefaultInboundPermissions is the deny-by-default ruleset for externally
// triggered sessions. A remote caller must not gain shell or file mutation
// power implicitly; hosts widen this explicitly (see docs/DESIGN-A2A.md §5.5).
func DefaultInboundPermissions() schema.PermissionRuleset {
	deny := schema.PermissionEffectDeny
	return schema.PermissionRuleset{
		{Action: "bash", Resource: "*", Effect: deny},
		{Action: "write", Resource: "*", Effect: deny},
		{Action: "edit", Resource: "*", Effect: deny},
		{Action: "apply_patch", Resource: "*", Effect: deny},
		{Action: "external_directory", Resource: "*", Effect: deny},
	}
}

// NewA2AFactory returns an adapter that lets the A2A server drive sessions of
// this client. It switches the client to out-of-band interaction mode
// (HoldInteractions), so permission and question prompts surface as A2A
// input-required instead of blocking on the default handlers.
//
// Inbound sessions run under DefaultInboundPermissions unless overridden with
// WithInboundPermissions: the client's own permission handler (which may be
// ApproveAll) does NOT grant a remote caller shell or file mutation power.
func NewA2AFactory(client *Client, opts ...A2AFactoryOption) server.Factory {
	config := A2AInboundConfig{Permissions: DefaultInboundPermissions()}
	for _, opt := range opts {
		opt(&config)
	}
	client.HoldInteractions(true)
	return &a2aFactory{client: client, permissions: config.Permissions}
}

// CreateSession implements server.Factory. The SDK client is bound to one
// project, so the workspace's Directory/Root are informational here; hosts that
// serve multiple workspaces implement server.Factory directly. The workspace's
// Permissions are applied when set.
func (f *a2aFactory) CreateSession(ctx context.Context, req server.SessionRequest) (server.Session, error) {
	session, err := f.client.CreateSession(
		ctx,
		&SessionConfig{Agent: req.AgentID, Permissions: f.permissionsFor(req.Workspace)},
	)
	if err != nil {
		return nil, err
	}
	f.client.rt.markInbound(schema.SessionID(session.ID()))
	return &a2aSession{session: session, turns: map[string]*a2aTurn{}}, nil
}

// ResumeSession implements server.Factory.
func (f *a2aFactory) ResumeSession(
	ctx context.Context,
	sessionID string,
	workspace server.Workspace,
) (server.Session, error) {
	session, err := f.client.ResumeSession(ctx, sessionID, &SessionConfig{Permissions: f.permissionsFor(workspace)})
	if err != nil {
		return nil, err
	}
	f.client.rt.markInbound(schema.SessionID(session.ID()))
	return &a2aSession{session: session, turns: map[string]*a2aTurn{}}, nil
}

// permissionsFor picks the ruleset a session runs under: the resolved
// workspace's when it supplies one, otherwise the factory's inbound default.
func (f *a2aFactory) permissionsFor(workspace server.Workspace) schema.PermissionRuleset {
	if len(workspace.Permissions) > 0 {
		return workspace.Permissions
	}
	return f.permissions
}

// a2aSession bridges *Session to server.Session. Each turn forwards its event
// stream through a channel keyed by turn id, so a parked turn and a later turn
// on the same session stay independent. The forwarder goroutine keeps the turn
// alive after the executor parks at input-required; Resume reads the remaining
// events from the same channel, and OnTurnEnd callbacks fire when it settles.
type a2aSession struct {
	session *Session

	mu    sync.Mutex
	turns map[string]*a2aTurn
}

// a2aTurn is the live state of one turn: its event channel and the callbacks to
// run once the turn settles.
type a2aTurn struct {
	ch    chan server.Event
	onEnd []func()
}

func (s *a2aSession) ID() string { return s.session.ID() }

func (s *a2aSession) OnTurnEnd(turnID string, fn func()) {
	s.mu.Lock()
	turn := s.turnLocked(turnID)
	turn.onEnd = append(turn.onEnd, fn)
	s.mu.Unlock()
}

func (s *a2aSession) Send(ctx context.Context, turnID string, req server.SendRequest) iter.Seq2[server.Event, error] {
	prompt, err := s.materialize(turnID, req)
	if err != nil {
		// No channel was opened; still drop the turn and run its callbacks so
		// the session is released.
		s.mu.Lock()
		turn := s.turnLocked(turnID)
		s.mu.Unlock()
		s.endTurn(turnID, turn, false)
		return func(yield func(server.Event, error) bool) {
			yield(server.Event{Kind: server.EventError, Err: err}, nil)
		}
	}
	ch := make(chan server.Event, 256)
	s.mu.Lock()
	turn := s.turnLocked(turnID)
	turn.ch = ch
	s.mu.Unlock()
	// The turn runs on a detached context: when the executor parks at
	// input-required it returns and a2a-go cancels the request context, but the
	// parked turn must stay alive until the client replies.
	turnCtx := context.WithoutCancel(ctx)
	go func() {
		defer s.endTurn(turnID, turn, true)
		for event, err := range s.session.Send(turnCtx, MessageOptions{Prompt: prompt}) {
			mapped, ok := a2aEventFrom(event)
			if ok && err == nil {
				ch <- mapped
			}
			if err != nil {
				ch <- server.Event{Kind: server.EventError, Err: err}
				return
			}
			if mapped.Kind == server.EventIdle {
				return
			}
		}
	}()
	return s.reader(turnID)
}

// endTurn closes a turn's channel (when one was opened), drops the turn, and
// runs its OnTurnEnd callbacks so the session can be released.
func (s *a2aSession) endTurn(turnID string, turn *a2aTurn, closeChannel bool) {
	if closeChannel && turn.ch != nil {
		close(turn.ch)
	}
	s.mu.Lock()
	callbacks := turn.onEnd
	delete(s.turns, turnID)
	s.mu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
}

// materialize writes raw inbound attachments into the workspace and appends
// their paths to the prompt, so the agent's tools can read them by path; URL
// attachments are referenced (not fetched).
func (s *a2aSession) materialize(turnID string, req server.SendRequest) (string, error) {
	if len(req.Files) == 0 {
		return req.Prompt, nil
	}
	root := filepath.Join(string(s.session.client.rt.location.Directory), ".a2a")
	dir := filepath.Join(root, "attachments", turnID)
	var builder strings.Builder
	builder.WriteString(req.Prompt)
	builder.WriteString("\n\n[attachments]")
	for _, file := range req.Files {
		switch {
		case len(file.Data) > 0:
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("write attachment: %w", err)
			}
			if err := ensureA2AIgnore(root); err != nil {
				return "", fmt.Errorf("write attachment: %w", err)
			}
			path := filepath.Join(dir, filepath.Base(file.Name))
			if err := os.WriteFile(path, file.Data, 0o644); err != nil {
				return "", fmt.Errorf("write attachment: %w", err)
			}
			fmt.Fprintf(&builder, "\n- %s", path)
		case file.URI != "":
			fmt.Fprintf(&builder, "\n- %s", file.URI)
		}
	}
	return builder.String(), nil
}

// ensureA2AIgnore drops a .gitignore inside the .a2a directory so attachments
// are not captured by git or the snapshot service (which honors repo ignores),
// without touching the project's own .gitignore or leaving the workspace.
func ensureA2AIgnore(root string) error {
	path := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("*\n"), 0o644)
}

func (s *a2aSession) Resume(ctx context.Context, turnID string) iter.Seq2[server.Event, error] {
	return s.reader(turnID)
}

func (s *a2aSession) Steer(prompt string) error { return s.session.Steer(prompt) }

// turnLocked returns the turn entry, creating it when absent. The caller must
// hold s.mu.
func (s *a2aSession) turnLocked(turnID string) *a2aTurn {
	turn := s.turns[turnID]
	if turn == nil {
		turn = &a2aTurn{}
		s.turns[turnID] = turn
	}
	return turn
}

// reader forwards the events of one turn's channel.
func (s *a2aSession) reader(turnID string) func(func(server.Event, error) bool) {
	return func(yield func(server.Event, error) bool) {
		s.mu.Lock()
		turn := s.turns[turnID]
		var ch chan server.Event
		if turn != nil {
			ch = turn.ch
		}
		s.mu.Unlock()
		if ch == nil {
			return
		}
		for event := range ch {
			if !yield(event, nil) {
				return
			}
		}
	}
}

func (s *a2aSession) ReplyPermission(requestID, reply string) error {
	return s.session.client.Permission().Reply(PermissionReplyInput{
		RequestID: requestID, Reply: PermissionReply(reply),
	})
}

func (s *a2aSession) ReplyQuestion(requestID string, answers []server.Answer) error {
	converted := make([]schema.QuestionAnswer, 0, len(answers))
	for _, answer := range answers {
		converted = append(converted, schema.QuestionAnswer(answer.Labels))
	}
	return s.session.client.Question().Reply(requestID, converted)
}

func (s *a2aSession) RejectQuestion(requestID string) error {
	return s.session.client.Question().Reject(requestID)
}

func (s *a2aSession) Abort() { s.session.Abort() }

func (s *a2aSession) Close() error {
	s.session.client.rt.unmarkInbound(schema.SessionID(s.session.ID()))
	return s.session.Close()
}

// a2aEventFrom maps an SDK event onto the A2A server's narrow event type. The
// bool is false for events that have no A2A mapping.
func a2aEventFrom(event Event) (server.Event, bool) {
	switch e := event.(type) {
	case TextDelta:
		return server.Event{Kind: server.EventText, Text: e.Delta}, true
	case TextEnded:
		return server.Event{Kind: server.EventText, Text: ""}, true
	case ReasoningDelta:
		return server.Event{Kind: server.EventReasoning, Text: e.Delta}, true
	case ReasoningEnded:
		return server.Event{}, false
	case ToolCalled:
		return server.Event{Kind: server.EventToolCalled, ToolName: e.Name, CallID: e.CallID, Input: e.Input}, true
	case ToolSuccess:
		return server.Event{Kind: server.EventToolResult, CallID: e.CallID, Result: e.Content, Structured: e.Structured}, true
	case ToolFailed:
		return server.Event{Kind: server.EventToolResult, CallID: e.CallID, Result: e.Error, IsError: true}, true
	case ToolProgress:
		return server.Event{Kind: server.EventToolProgress, CallID: e.CallID, Result: e.Content}, true
	case StepStarted:
		return server.Event{Kind: server.EventStepStarted}, true
	case Usage:
		return server.Event{Kind: server.EventUsage, Usage: e.ProviderMetadata}, true
	case PermissionAsked:
		return server.Event{Kind: server.EventAwaitInput, RequestID: e.Request.ID}, true
	case QuestionAsked:
		return server.Event{
			Kind:       server.EventAwaitInput,
			RequestID:  e.Request.ID,
			IsQuestion: true,
			Questions:  e.Request.Questions,
		}, true
	case ErrorEvent:
		return server.Event{Kind: server.EventError, Err: e.Err}, true
	case Idle:
		return server.Event{Kind: server.EventIdle}, true
	default:
		return server.Event{}, false
	}
}
