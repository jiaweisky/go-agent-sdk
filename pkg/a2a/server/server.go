package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
	coresession "github.com/jiaweisky/go-agent-sdk/pkg/session"
)

// Options configures a Server.
type Options struct {
	// Factory creates and resumes SDK sessions for exposed agents.
	Factory Factory
	// Turns persists A2A tasks on the core turn table. When nil, an in-memory
	// store is used.
	Turns coresession.TurnStore
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
	// Security, when set, requires authentication on the JSON-RPC endpoints
	// (cards stay public). Nil disables authentication.
	Security *Security
	// Ingest, when set, enables the A2A file-exchange edge: each card advertises
	// a storage extension, and PUT/GET ingest endpoints carry large files out of
	// band. Nil disables file exchange.
	Ingest *Ingest
	// MaxBodyBytes, when > 0, caps a JSON-RPC request body; a larger one is
	// rejected with 413 instead of being buffered whole (protects against an
	// oversized inline file part). Out-of-band ingest uploads are bounded
	// separately by Ingest.MaxBytes. Zero leaves it unbounded.
	MaxBodyBytes int64
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

// DiscoveryPath serves every exposed agent's card as a JSON array. CodeBuddy's
// CODEBUDDY_REMOTE_AGENTS_URL discovery source expects exactly that shape (a
// top-level array or {"cards":[...]}), which the A2A-standard well-known path —
// a single card — does not provide. The well-known path stays single (see
// Handler); this is an additive, non-standard endpoint.
const DiscoveryPath = "/.well-known/agents.json"

// New builds a server for the exposed agents in Options.Agents.
func New(opts Options) (*Server, error) {
	if opts.Factory == nil {
		return nil, fmt.Errorf("a2a/server: Factory is required")
	}
	if opts.Security != nil && opts.Security.Bearer == "" {
		return nil, fmt.Errorf("a2a/server: Security.Bearer is required")
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
		mux.Handle(s.endpointPath(info), s.secure(s.maxBody(s.rpcHandler(info))))
		if s.opts.Ingest != nil && s.opts.Ingest.Store != nil {
			mux.Handle(s.ingestPath(info), s.secure(s.ingestHandler(info)))
		}
	}
	// The default well-known path serves the first exposed agent so a client
	// that only knows the host can discover it.
	mux.Handle("/.well-known/agent-card.json", s.cardHandler(s.exposed[0]))
	// The aggregate path lists every exposed agent for discovery sources that
	// want all cards at once (e.g. CodeBuddy's CODEBUDDY_REMOTE_AGENTS_URL).
	mux.Handle(DiscoveryPath, s.cardsHandler())
	return mux
}

func (s *Server) endpointPath(info schema.AgentInfo) string {
	return "/agents/" + string(info.ID) + "/"
}

// maxBody caps a request body to MaxBodyBytes (when set): a declared oversized
// Content-Length is rejected with 413 up front, and a chunked body is bounded by
// MaxBytesReader, so a JSON-RPC request cannot buffer an unbounded inline part.
func (s *Server) maxBody(next http.Handler) http.Handler {
	limit := s.opts.MaxBodyBytes
	if limit <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > limit {
			http.Error(w, fmt.Sprintf("request body exceeds the %d-byte limit", limit), http.StatusRequestEntityTooLarge)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cardPath(info schema.AgentInfo) string {
	if info.Expose != nil && info.Expose.Path != "" {
		return info.Expose.Path
	}
	return "/agents/" + string(info.ID) + "/.well-known/agent-card.json"
}

func (s *Server) cardHandler(info schema.AgentInfo) http.Handler {
	producer := a2asrv.AgentCardProducerFn(func(ctx context.Context) (*a2a.AgentCard, error) {
		return buildCard(info, s.cardEndpoint(ctx, info), s.opts.Security, s.storageExtension(ctx, info)), nil
	})
	inner := a2asrv.NewAgentCardHandler(producer)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(withRequest(r.Context(), r)))
	})
}

// cardsHandler serves every exposed agent's card as a JSON array. Each card's
// endpoint is resolved from the request's own origin (or the configured
// BaseURL), exactly like the single-card handler, so the URLs stay correct
// behind a proxy.
func (s *Server) cardsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Cards(r))
	})
}

// Cards returns the AgentCard for every exposed agent. Each card's endpoint is
// resolved from the configured BaseURL, or from r's origin when BaseURL is unset;
// with neither, endpoints are returned relative. This lets a caller (a
// multiplexing host, for example) aggregate cards across several server
// instances onto one origin; r may be nil when every instance sets BaseURL.
func (s *Server) Cards(r *http.Request) []*a2a.AgentCard {
	ctx := context.Background()
	if r != nil {
		ctx = withRequest(r.Context(), r)
	}
	cards := make([]*a2a.AgentCard, 0, len(s.exposed))
	for _, info := range s.exposed {
		cards = append(cards, buildCard(info, s.cardEndpoint(ctx, info), s.opts.Security, s.storageExtension(ctx, info)))
	}
	return cards
}

// CardPaths returns each exposed agent's card path (where its AgentCard is
// served), in exposure order.
func (s *Server) CardPaths() []string {
	paths := make([]string, 0, len(s.exposed))
	for _, info := range s.exposed {
		paths = append(paths, s.cardPath(info))
	}
	return paths
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

// storageExtension returns the card's storage extension, or nil when the
// file-exchange edge is disabled.
func (s *Server) storageExtension(ctx context.Context, info schema.AgentInfo) *a2a.AgentExtension {
	if s.opts.Ingest == nil || s.opts.Ingest.Store == nil {
		return nil
	}
	ext := s.opts.Ingest.extension(s.ingestEndpoint(ctx, info))
	return &ext
}

// ingestEndpoint resolves the agent's ingest endpoint (where a peer uploads),
// mirroring cardEndpoint's origin handling.
func (s *Server) ingestEndpoint(ctx context.Context, info schema.AgentInfo) string {
	if s.opts.BaseURL != "" {
		return s.opts.BaseURL + s.ingestPath(info)
	}
	if r, ok := requestFromContext(ctx); ok {
		return requestBaseURL(r) + s.ingestPath(info)
	}
	return s.ingestPath(info)
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
