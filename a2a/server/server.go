package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	coresession "github.com/jiaweisky/go-agent-sdk/core/session"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// Options configures a Server.
type Options struct {
	// Factory creates and resumes SDK sessions for exposed agents.
	Factory Factory
	// Turns persists A2A tasks on the core turn table. When nil, an in-memory
	// store is used.
	Turns *coresession.TurnStore
	// TaskStore overrides the task store. Takes precedence over Turns.
	TaskStore taskstore.Store
	// Agents lists the agents whose card this server exposes. Only agents with
	// Expose set are served.
	Agents []schema.AgentInfo
	// BaseURL is the public base URL of the server, prefixed to each agent's
	// endpoint in its card's supportedInterfaces. When empty the handler derives
	// it from each request's scheme and host (honoring X-Forwarded-Proto/Host),
	// so the card stays usable behind a proxy without configuration.
	BaseURL string
	// KeepAlive sets the SSE transport keep-alive interval. Zero disables it.
	KeepAlive time.Duration
	// Workspaces resolves the workspace (project/root/permissions) an inbound
	// call runs in. Nil uses the factory's defaults for every call.
	Workspaces WorkspaceResolver
	// Artifacts optionally externalizes or rewrites emitted artifacts.
	Artifacts ArtifactExternalizer
	// NativeOptions are passed to the underlying A2A server for capabilities the
	// SDK does not model: authentication interceptors, concurrency limits, push
	// notifications, and so on. Each value must be an
	// a2asrv.RequestHandlerOption or an a2asrv.TransportOption; other values are
	// ignored. The signature uses any so a2a-go types never enter the SDK API.
	NativeOptions []any
}

// Server exposes SDK agents over A2A.
type Server struct {
	opts    Options
	store   taskstore.Store
	exposed []schema.AgentInfo
}

// New builds a server for the exposed agents in Options.Agents.
func New(opts Options) (*Server, error) {
	if opts.Factory == nil {
		return nil, fmt.Errorf("a2a/server: Factory is required")
	}
	store := opts.TaskStore
	if store == nil {
		if opts.Turns == nil {
			store = taskstore.NewInMemory(nil)
		} else {
			store = NewTurnTaskStore(opts.Turns)
		}
	}
	server := &Server{opts: opts, store: store}
	for _, info := range opts.Agents {
		if info.Expose == nil {
			continue
		}
		server.exposed = append(server.exposed, info)
	}
	if len(server.exposed) == 0 {
		return nil, fmt.Errorf("a2a/server: no exposed agents (set AgentInfo.Expose)")
	}
	return server, nil
}

// Handler returns the http.Handler serving the exposed agents' cards and a
// JSON-RPC endpoint per agent.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, info := range s.exposed {
		mux.Handle(s.cardPath(info), s.cardHandler(info))
		mux.Handle(s.endpointPath(info), s.rpcHandler(info))
	}
	// The default well-known path serves the first exposed agent so a client
	// that only knows the host can discover it.
	mux.Handle("/.well-known/agent-card.json", s.cardHandler(s.exposed[0]))
	return mux
}

func (s *Server) endpointPath(info schema.AgentInfo) string {
	return "/agents/" + string(info.ID) + "/"
}

func (s *Server) cardPath(info schema.AgentInfo) string {
	if info.Expose != nil && info.Expose.Path != "" {
		return info.Expose.Path
	}
	return "/agents/" + string(info.ID) + "/.well-known/agent-card.json"
}

func (s *Server) cardHandler(info schema.AgentInfo) http.Handler {
	producer := a2asrv.AgentCardProducerFn(func(ctx context.Context) (*a2a.AgentCard, error) {
		return buildCard(info, s.cardEndpoint(ctx, info)), nil
	})
	inner := a2asrv.NewAgentCardHandler(producer)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(withRequest(r.Context(), r)))
	})
}

// cardEndpoint resolves the absolute JSON-RPC URL advertised in a card: the
// configured BaseURL when set, otherwise the request's own origin.
func (s *Server) cardEndpoint(ctx context.Context, info schema.AgentInfo) string {
	if s.opts.BaseURL != "" {
		return s.endpointURL(info)
	}
	if r, ok := requestFromContext(ctx); ok {
		return requestBaseURL(r) + s.endpointPath(info)
	}
	return s.endpointPath(info)
}

// requestBaseURL reconstructs the scheme and host a request arrived on,
// preferring reverse-proxy headers.
func requestBaseURL(r *http.Request) string {
	scheme := ""
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = firstForwarded(forwarded)
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = firstForwarded(forwarded)
	}
	return scheme + "://" + host
}

// firstForwarded returns the first value of a comma-separated proxy header.
func firstForwarded(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}

// requestContextKey carries the *http.Request to a card producer, which a2a-go
// invokes with only the request context.
type requestContextKey struct{}

func withRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, requestContextKey{}, r)
}

func requestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestContextKey{}).(*http.Request)
	return r, ok
}

func (s *Server) rpcHandler(info schema.AgentInfo) http.Handler {
	exec := newExecutor(executorConfig{
		factory:    s.opts.Factory,
		agentID:    string(info.ID),
		store:      s.store,
		workspaces: s.opts.Workspaces,
		artifacts:  s.opts.Artifacts,
	})
	capabilities := &a2a.AgentCapabilities{Streaming: true}
	handlerOptions := []a2asrv.RequestHandlerOption{
		a2asrv.WithTaskStore(s.store),
		a2asrv.WithCapabilityChecks(capabilities),
	}
	var transportOptions []a2asrv.TransportOption
	if s.opts.KeepAlive > 0 {
		transportOptions = append(transportOptions, a2asrv.WithTransportKeepAlive(s.opts.KeepAlive))
	}
	for _, option := range s.opts.NativeOptions {
		switch typed := option.(type) {
		case a2asrv.RequestHandlerOption:
			handlerOptions = append(handlerOptions, typed)
		case a2asrv.TransportOption:
			transportOptions = append(transportOptions, typed)
		}
	}
	return a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(exec, handlerOptions...), transportOptions...)
}

func (s *Server) endpointURL(info schema.AgentInfo) string {
	path := s.endpointPath(info)
	if s.opts.BaseURL == "" {
		return path
	}
	return s.opts.BaseURL + path
}

// nowMillis returns the current Unix time in milliseconds.
func nowMillis() int64 {
	return time.Now().UnixMilli()
}
