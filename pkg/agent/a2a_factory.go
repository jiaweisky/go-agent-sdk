package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"

	coresession "github.com/jiaweisky/go-agent-sdk/internal/core/session"
	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/session"
)

// TurnStore returns a task store over the client's database, so an A2A server
// persists tasks durably (and survives restarts). Pass it to
// server.Options.Turns.
func (c *Client) TurnStore() session.TurnStore {
	return coresession.NewTurnStore(c.rt.db)
}

// a2aFactory adapts a Client to the A2A server's session surface. It binds the
// inbound permissions every externally triggered session runs under.
type a2aFactory struct {
	client       *Client
	permissions  schema.PermissionRuleset
	inboundMedia string
	// spoolDir is where spooled inbound attachments are written (under the state
	// root's data directory, never the workspace).
	spoolDir string
	// questions is "park" (default) or "decline".
	questions string
}

// A2AInboundConfig configures how an A2A server drives sessions.
type A2AInboundConfig struct {
	// Permissions is the ruleset applied to sessions created by inbound A2A
	// calls.
	Permissions schema.PermissionRuleset
	// InboundMedia is "spool" (default) or "inline". With "inline", known media
	// parts with bytes become native media on the turn; otherwise they are
	// written into the workspace and referenced by path.
	InboundMedia string
	// Questions selects what happens when the model invokes the question tool:
	// "park" (default) holds the turn at input-required until the client
	// answers; "decline" rejects every question, ending the turn with the
	// dismissal instead of parking it forever when no client will ever answer.
	Questions string
}

// A2AFactoryOption customizes NewA2AFactory.
type A2AFactoryOption func(*A2AInboundConfig)

// WithInboundPermissions overrides the ruleset applied to externally triggered
// sessions.
func WithInboundPermissions(rules schema.PermissionRuleset) A2AFactoryOption {
	return func(config *A2AInboundConfig) { config.Permissions = rules }
}

// WithInboundMedia selects how inbound A2A media is delivered: "spool" (default)
// writes files into the workspace; "inline" delivers known media as native parts
// when the target model accepts the modality.
func WithInboundMedia(mode string) A2AFactoryOption {
	return func(config *A2AInboundConfig) { config.InboundMedia = mode }
}

// WithInboundQuestions selects the question tool's policy: "park" (default)
// holds the turn at input-required until the client answers; "decline" rejects
// questions so the turn ends with the dismissal instead of parking forever.
func WithInboundQuestions(mode string) A2AFactoryOption {
	return func(config *A2AInboundConfig) { config.Questions = mode }
}

// DefaultInboundPermissions is the deny-by-default ruleset for externally
// triggered sessions. A remote caller must not gain shell or file mutation
// power implicitly; hosts widen this explicitly.
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
// (HoldInteractions), so permission prompts surface as A2A input-required
// instead of blocking on the default handlers.
//
// With the "decline" questions policy, questions leave hold mode immediately:
// a question ask resolves with a rejection, which ends the turn with the
// dismissal instead of parking it at input-required. Permissions stay held —
// an approval is a meaningful client interaction.
//
// Inbound sessions run under DefaultInboundPermissions unless overridden with
// WithInboundPermissions: the client's own permission handler (which may be
// ApproveAll) does NOT grant a remote caller shell or file mutation power.
func NewA2AFactory(client *Client, opts ...A2AFactoryOption) server.Factory {
	config := A2AInboundConfig{Permissions: DefaultInboundPermissions(), InboundMedia: "spool"}
	for _, opt := range opts {
		opt(&config)
	}
	client.HoldInteractions(true)
	if config.Questions == "decline" {
		// Permissions stay held; questions resolve through handlers again.
		client.HoldQuestions(false)
	}
	return &a2aFactory{
		client:       client,
		permissions:  config.Permissions,
		inboundMedia: config.InboundMedia,
		spoolDir:     core.A2ASpoolDir(client.rt.global.Data),
		questions:    config.Questions,
	}
}

// CreateSession implements server.Factory. The SDK client is bound to one
// project, so the workspace's Directory/Root are informational here; hosts that
// serve multiple workspaces implement server.Factory directly. The workspace's
// Permissions are applied when set.
func (f *a2aFactory) CreateSession(ctx context.Context, req server.SessionRequest) (server.Session, error) {
	session, err := f.client.CreateSession(
		ctx,
		&SessionConfig{Agent: req.AgentID, Permissions: f.permissionsFor(req.Workspace), OnQuestion: f.questionOverride()},
	)
	if err != nil {
		return nil, err
	}
	f.client.rt.markInbound(schema.SessionID(session.ID()))
	return &a2aSession{session: session, turns: map[string]*a2aTurn{}, inboundMedia: f.inboundMedia, spoolDir: f.spoolDir}, nil
}

// ResumeSession implements server.Factory. The inbound/workspace ruleset is
// merged over the session's stored rules rather than replacing them, so a
// session a host created in-process keeps its own policy while still being
// constrained by the inbound rules.
func (f *a2aFactory) ResumeSession(
	ctx context.Context,
	sessionID string,
	workspace server.Workspace,
) (server.Session, error) {
	inbound := f.permissionsFor(workspace)
	stored, err := f.client.rt.store.Get(schema.SessionID(sessionID))
	if err != nil {
		return nil, err
	}
	rules := inbound
	if stored != nil {
		rules = mergePermissionRules(permissionRulesOf(stored), inbound)
	}
	session, err := f.client.ResumeSession(ctx, sessionID, &SessionConfig{Permissions: rules, OnQuestion: f.questionOverride()})
	if err != nil {
		return nil, err
	}
	f.client.rt.markInbound(schema.SessionID(session.ID()))
	return &a2aSession{session: session, turns: map[string]*a2aTurn{}, inboundMedia: f.inboundMedia, spoolDir: f.spoolDir}, nil
}

// questionOverride returns the per-session question handler enforcing the
// "decline" policy: nil in park mode (the host's handler applies), or a
// rejection handler in decline mode. A rejection ends the turn with the
// dismissal, so an unattended session never parks waiting for an answer
// nobody will give.
func (f *a2aFactory) questionOverride() QuestionHandlerFunc {
	if f.questions == "decline" {
		return RejectQuestions
	}
	return nil
}

// mergePermissionRules appends the overlay rules not already present in base, so
// the result is idempotent (re-merging the same overlay does not grow the
// ruleset) and the overlay wins on conflict, since evaluation takes the last
// matching rule.
func mergePermissionRules(base, overlay schema.PermissionRuleset) schema.PermissionRuleset {
	if len(overlay) == 0 {
		return base
	}
	if len(base) == 0 {
		return overlay
	}
	merged := make(schema.PermissionRuleset, 0, len(base)+len(overlay))
	merged = append(merged, base...)
	for _, rule := range overlay {
		if !containsPermissionRule(merged, rule) {
			merged = append(merged, rule)
		}
	}
	return merged
}

// containsPermissionRule reports whether rules already contains an identical
// rule (Action, Resource, Effect).
func containsPermissionRule(rules schema.PermissionRuleset, rule schema.PermissionRule) bool {
	for _, existing := range rules {
		if existing == rule {
			return true
		}
	}
	return false
}

// permissionsFor picks the ruleset a session runs under: the resolved
// workspace's when it supplies one, otherwise the factory's inbound default. The
// SDK spool directory is always appended as an external_directory allow, so an
// inbound session (which denies external_directory) can still read the
// attachment the caller sent — the rule follows the deny, so it wins.
func (f *a2aFactory) permissionsFor(workspace server.Workspace) schema.PermissionRuleset {
	rules := f.permissions
	if len(workspace.Permissions) > 0 {
		rules = workspace.Permissions
	}
	return append(append(schema.PermissionRuleset{}, rules...), schema.PermissionRule{
		Action:   "external_directory",
		Resource: filepath.Join(f.spoolDir, "*"),
		Effect:   schema.PermissionEffectAllow,
	})
}

// a2aSession bridges *Session to server.Session. Each turn forwards its event
// stream through a channel keyed by turn id, so a parked turn and a later turn
// on the same session stay independent. The forwarder goroutine keeps the turn
// alive after the executor parks at input-required; Resume reads the remaining
// events from the same channel, and OnTurnEnd callbacks fire when it settles.
type a2aSession struct {
	session *Session

	// inboundMedia is "spool" or "inline".
	inboundMedia string
	// spoolDir is where "spool" mode writes inbound attachments.
	spoolDir string

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
	prompt, files, err := s.materialize(turnID, req)
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
		for event, err := range s.session.Send(turnCtx, MessageOptions{Prompt: prompt, Files: files}) {
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

// materialize prepares inbound attachments. In "spool" mode (default) raw
// attachments are written into the workspace and referenced by path, so the
// agent's tools can read them; URL attachments are referenced (not fetched). In
// "inline" mode known media with bytes become native media parts and URL parts
// become url-source parts, so the target model's planner decides whether they
// are carried.
func (s *a2aSession) materialize(turnID string, req server.SendRequest) (string, []schema.PromptFileAttachment, error) {
	if len(req.Files) == 0 {
		return req.Prompt, nil, nil
	}
	inline := s.inboundMedia == "inline"
	dir := filepath.Join(s.spoolDir, turnID)
	var files []schema.PromptFileAttachment
	var builder strings.Builder
	builder.WriteString(req.Prompt)
	builder.WriteString("\n\n[attachments]")
	for _, file := range req.Files {
		attachment, note, err := materializeA2AFile(dir, inline, file)
		if err != nil {
			return "", nil, err
		}
		if attachment != nil {
			files = append(files, *attachment)
		}
		if note != "" {
			fmt.Fprintf(&builder, "\n- %s", note)
		}
	}
	return builder.String(), files, nil
}

// materializeA2AFile prepares one inbound A2A file. In inline mode, known media
// with bytes become a native attachment and a URL with a MIME becomes a
// url-source attachment; otherwise raw bytes are written into dir and referenced
// by path. It returns the native attachment (nil when none applies) and the
// prompt note describing the file.
func materializeA2AFile(dir string, inline bool, file server.File) (*schema.PromptFileAttachment, string, error) {
	name := file.Name
	if name == "" {
		name = file.MIME
	}
	switch {
	case len(file.Data) > 0:
		if inline {
			return &schema.PromptFileAttachment{
				URI:  fmt.Sprintf("data:%s;base64,%s", file.MIME, base64.StdEncoding.EncodeToString(file.Data)),
				Mime: file.MIME,
				Name: &name,
			}, name + " (attached)", nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", fmt.Errorf("write attachment: %w", err)
		}
		path := filepath.Join(dir, filepath.Base(name))
		if err := os.WriteFile(path, file.Data, 0o644); err != nil {
			return nil, "", fmt.Errorf("write attachment: %w", err)
		}
		return nil, path, nil
	case file.URI != "":
		if inline && file.MIME != "" {
			return &schema.PromptFileAttachment{URI: file.URI, Mime: file.MIME, Name: &name}, file.URI, nil
		}
		return nil, file.URI, nil
	}
	return nil, "", nil
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
		return server.Event{
			Kind:      server.EventAwaitInput,
			RequestID: e.Request.ID,
			Permission: &server.PermissionInfo{
				Action:    e.Request.Action,
				Resources: e.Request.Resources,
				Save:      e.Request.Save,
			},
		}, true
	case QuestionAsked:
		return server.Event{
			Kind:       server.EventAwaitInput,
			RequestID:  e.Request.ID,
			IsQuestion: true,
			Questions:  e.Request.Questions,
		}, true
	case ErrorEvent:
		return server.Event{Kind: server.EventError, Err: e.Err}, true
	case AttachmentOmitted:
		return server.Event{
			Kind: server.EventNotice,
			Text: fmt.Sprintf("attachment omitted: %s (%s; %s)", e.Name, e.MIME, e.Reason),
		}, true
	case Idle:
		return server.Event{Kind: server.EventIdle}, true
	default:
		return server.Event{}, false
	}
}
