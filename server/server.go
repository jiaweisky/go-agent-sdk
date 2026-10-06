// Package server exposes go-agent-sdk over a language-agnostic HTTP/SSE API so
// hosts written in other languages can drive the SDK across a process boundary.
// See docs/DESIGN-HTTP.md.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// Options configures a Server. It covers the HTTP surface only: every model/SDK
// setting lives on the injected *agent.Client.
type Options struct {
	// Auth, when set, requires a credential on every /api route except Public.
	// Nil disables authentication (loopback use).
	Auth *Auth
	// Agents is the allow-list of agent names the API may create/use. Empty
	// allows every registered agent.
	Agents []string
	// CORS adds browser origins to the default allow-list.
	CORS []string
	// InboundPermissions is applied to sessions created through the API, merged
	// over the agent's ruleset. Zero uses agent.DefaultInboundPermissions().
	InboundPermissions schema.PermissionRuleset
	// InteractionTimeout, when > 0, auto-resolves a held permission/question
	// request that no client answers in time.
	InteractionTimeout time.Duration
	// InteractionFallback is the auto-resolution on timeout: "reject" (default)
	// or "allow".
	InteractionFallback string
	// SubscriberCapacity bounds the live event queue per SSE subscriber. Zero
	// uses the default (256).
	SubscriberCapacity int
	// KeepAlive is the SSE heartbeat interval. Negative disables it; zero uses
	// the default (15s).
	KeepAlive time.Duration
	// Public lists route patterns that skip Auth.
	Public []string
}

// Server exposes one agent.Client over HTTP.
type Server struct {
	client    *agent.Client
	opts      Options
	allow     map[string]bool // nil means allow all
	inbound   schema.PermissionRuleset
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once

	mu      sync.Mutex
	entries map[string]*sessionEntry
}

type sessionEntry struct {
	handle *agent.Session
	worker *worker
	mu     sync.Mutex
	refs   int
}

// New binds a Server to one Client. It switches the client to hold mode so
// permission/question prompts surface over HTTP.
func New(client *agent.Client, opts Options) (*Server, error) {
	if client == nil {
		return nil, errors.New("server: client is required")
	}
	if opts.Auth != nil {
		switch opts.Auth.Scheme {
		case "", schemeBearer:
			if opts.Auth.Token == "" {
				return nil, errors.New("server: Auth.Token is required for bearer")
			}
		case schemeBasic:
			if opts.Auth.Password == "" {
				return nil, errors.New("server: Auth.Password is required for basic")
			}
		default:
			return nil, fmt.Errorf("server: unknown Auth.Scheme %q", opts.Auth.Scheme)
		}
	}
	if opts.InteractionFallback == "" {
		opts.InteractionFallback = "reject"
	}
	if opts.InteractionFallback != "reject" && opts.InteractionFallback != "allow" {
		return nil, fmt.Errorf("server: InteractionFallback must be reject or allow, got %q", opts.InteractionFallback)
	}
	if opts.SubscriberCapacity == 0 {
		opts.SubscriberCapacity = 256
	}
	if opts.SubscriberCapacity < 0 {
		return nil, errors.New("server: SubscriberCapacity must not be negative")
	}
	if opts.KeepAlive == 0 {
		opts.KeepAlive = 15 * time.Second
	}
	inbound := opts.InboundPermissions
	if len(inbound) == 0 {
		inbound = agent.DefaultInboundPermissions()
	}

	s := &Server{
		client:  client,
		opts:    opts,
		inbound: inbound,
		entries: map[string]*sessionEntry{},
	}
	if len(opts.Agents) > 0 {
		s.allow = make(map[string]bool, len(opts.Agents))
		for _, name := range opts.Agents {
			if client.Agents().Get(schema.AgentID(name)) == nil {
				return nil, fmt.Errorf("server: unknown agent %q", name)
			}
			s.allow[name] = true
		}
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	client.HoldInteractions(true)
	go s.runWatcher(s.ctx)
	return s, nil
}

// Handler returns the http.Handler for the host to mount on its own mux/server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/agent", s.handleAgentList)
	mux.HandleFunc("GET /api/skill", s.handleSkillList)
	mux.HandleFunc("GET /api/tool", s.handleToolList)

	mux.HandleFunc("GET /api/session", s.handleSessionList)
	mux.HandleFunc("POST /api/session", s.handleSessionCreate)
	mux.HandleFunc("GET /api/session/{sessionID}", s.handleSessionGet)
	mux.HandleFunc("DELETE /api/session/{sessionID}", s.handleSessionDelete)
	mux.HandleFunc("POST /api/session/{sessionID}/agent", s.handleSessionSetAgent)
	mux.HandleFunc("POST /api/session/{sessionID}/model", s.handleSessionSetModel)
	mux.HandleFunc("POST /api/session/{sessionID}/title", s.handleSessionSetTitle)
	mux.HandleFunc("POST /api/session/{sessionID}/prompt", s.handleSessionPrompt)
	mux.HandleFunc("POST /api/session/{sessionID}/interrupt", s.handleSessionInterrupt)
	mux.HandleFunc("POST /api/session/{sessionID}/compact", s.handleSessionCompact)
	mux.HandleFunc("POST /api/session/{sessionID}/wait", s.handleSessionWait)
	mux.HandleFunc("GET /api/session/{sessionID}/message", s.handleSessionMessage)
	mux.HandleFunc("GET /api/session/{sessionID}/context", s.handleSessionContext)
	mux.HandleFunc("GET /api/session/{sessionID}/event", s.handleSessionEvent)
	mux.HandleFunc("POST /api/session/{sessionID}/revert/stage", s.handleSessionRevertStage)
	mux.HandleFunc("GET /api/session/{sessionID}/revert", s.handleSessionRevertGet)
	mux.HandleFunc("POST /api/session/{sessionID}/revert/clear", s.handleSessionRevertClear)
	mux.HandleFunc("POST /api/session/{sessionID}/revert/commit", s.handleSessionRevertCommit)

	mux.HandleFunc("GET /api/permission", s.handlePermissionList)
	mux.HandleFunc("GET /api/session/{sessionID}/permission", s.handleSessionPermissionList)
	mux.HandleFunc("GET /api/session/{sessionID}/permission/{requestID}", s.handleSessionPermissionGet)
	mux.HandleFunc("POST /api/session/{sessionID}/permission/{requestID}/reply", s.handleSessionPermissionReply)
	mux.HandleFunc("GET /api/question", s.handleQuestionList)
	mux.HandleFunc("GET /api/session/{sessionID}/question", s.handleSessionQuestionList)
	mux.HandleFunc("GET /api/session/{sessionID}/question/{requestID}", s.handleSessionQuestionGet)
	mux.HandleFunc("POST /api/session/{sessionID}/question/{requestID}/reply", s.handleSessionQuestionReply)
	mux.HandleFunc("POST /api/session/{sessionID}/question/{requestID}/reject", s.handleSessionQuestionReject)

	mux.HandleFunc("GET /api/event", s.handleEvent)

	var h http.Handler = mux
	h = s.withJSONErrors(h)
	h = s.withAuth(h)
	h = s.withCORS(h)
	h = s.withRecover(h)
	return h
}

// Close cancels SSE streams and signals workers to stop. It does not close the
// client; the host owns it.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		entries := make([]*sessionEntry, 0, len(s.entries))
		for _, e := range s.entries {
			entries = append(entries, e)
		}
		s.entries = map[string]*sessionEntry{}
		s.mu.Unlock()
		for _, e := range entries {
			e.mu.Lock()
			if e.handle != nil {
				_ = e.handle.Close()
			}
			e.mu.Unlock()
		}
	})
	return nil
}

// allowed reports whether an agent may be used.
func (s *Server) allowed(name string) bool {
	if s.allow == nil {
		return true
	}
	return s.allow[name]
}

// defaultAgent is the agent used when a create request omits one: the first
// allow-list entry, or the SDK default agent when no list is configured.
func (s *Server) defaultAgent() string {
	if len(s.opts.Agents) > 0 {
		return s.opts.Agents[0]
	}
	return string(core.AgentDefaultID)
}

// attach returns the shared handle for a session, resuming it once if needed.
// The inbound ruleset is merged over the session's stored rules.
func (s *Server) attach(ctx context.Context, sessionID string) (*agent.Session, error) {
	e := s.entry(sessionID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handle != nil {
		return e.handle, nil
	}
	info, err := s.client.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	rules := s.inbound
	if info.Permission != nil {
		rules = core.PermissionMerge(*info.Permission, s.inbound)
	}
	handle, err := s.client.ResumeSession(ctx, sessionID, &agent.SessionConfig{Permissions: rules})
	if err != nil {
		return nil, err
	}
	e.handle = handle
	return handle, nil
}

// withHandle runs fn against the session's handle, keeping the handle alive for
// the duration.
func (s *Server) withHandle(ctx context.Context, sessionID string, fn func(*agent.Session) error) error {
	e := s.entry(sessionID)
	e.mu.Lock()
	e.refs++
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.refs--
		e.mu.Unlock()
		s.maybeEvict(sessionID)
	}()
	handle, err := s.attach(ctx, sessionID)
	if err != nil {
		return err
	}
	return fn(handle)
}

func (s *Server) entry(id string) *sessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[id]
	if e == nil {
		e = &sessionEntry{}
		s.entries[id] = e
	}
	return e
}

// maybeEvict closes and drops a session's handle when it is idle and unused.
func (s *Server) maybeEvict(id string) {
	s.mu.Lock()
	e := s.entries[id]
	s.mu.Unlock()
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.worker == nil && e.refs == 0 && e.handle != nil {
		_ = e.handle.Close()
		e.handle = nil
		s.mu.Lock()
		delete(s.entries, id)
		s.mu.Unlock()
	}
	e.mu.Unlock()
}
