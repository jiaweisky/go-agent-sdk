package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
	coresession "github.com/jiaweisky/go-agent-sdk/pkg/session"
)

// PushNotificationsConfig enables push notifications. The client supplies the
// per-task webhook configuration; this configures the server's sender.
// Configurations are stored in memory and are lost on restart.
type PushNotificationsConfig struct {
	// Timeout bounds one webhook POST. Zero uses the SDK default (30s).
	Timeout time.Duration
	// AllowPrivateNetworks disables the SSRF guard for trusted internal webhooks.
	AllowPrivateNetworks bool
	// FailOnError cancels task execution when a notification fails.
	FailOnError bool
}

// SigningConfig configures AgentCard signing with one private key.
type SigningConfig struct {
	PrivateKey crypto.Signer
	KeyID      string
	Algorithm  string
	JWKSURL    string
}

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
	// PathPrefix is the URL path prefix the handler is mounted under. It is
	// appended to endpoint URLs in cards when BaseURL is empty, so a handler
	// mounted with http.StripPrefix still advertises the reachable URL. BaseURL,
	// when set, is authoritative and must already include the prefix.
	PathPrefix string
	// Transports lists the protocol bindings to serve. Empty means JSON-RPC only;
	// duplicates and unknown values are rejected.
	Transports []Transport
	// PushNotifications enables push notifications and derives the card's
	// pushNotifications capability.
	PushNotifications *PushNotificationsConfig
	// ExtendedCards maps an exposed agent to its authenticated extended card.
	// Presence derives the card's extendedAgentCard capability. Keys must name
	// exposed agents.
	ExtendedCards map[schema.AgentID]schema.AgentExpose
	// Signing, when set, signs every served card. Signatures are computed.
	Signing *SigningConfig
	// OnError reports non-fatal card-production errors, such as a signing
	// failure while building aggregate discovery. Nil ignores them.
	OnError func(error)
	// KeepAlive sets the SSE transport keep-alive interval. Zero disables it.
	KeepAlive time.Duration
	// Workspaces resolves the workspace (project/root/permissions) an inbound
	// call runs in. Nil uses the factory's defaults for every call.
	Workspaces WorkspaceResolver
	// Artifacts optionally externalizes or rewrites emitted artifacts.
	Artifacts ArtifactExternalizer
	// Security, when set, requires authentication on the RPC endpoints
	// (JSON-RPC and REST; cards stay public). Nil disables authentication.
	Security *Security
	// MaxBodyBytes, when > 0, caps an RPC request body; a larger one is
	// rejected with 413 instead of being buffered whole (protects against an
	// oversized inline file part). Zero leaves it unbounded; negative is invalid.
	MaxBodyBytes int64
	// NativeOptions are passed to the underlying A2A server for capabilities the
	// SDK does not model: authentication interceptors, concurrency limits, and
	// so on. Each value must be an
	// a2asrv.RequestHandlerOption or an a2asrv.TransportOption; other values are
	// ignored. The signature uses any so a2a-go types never enter the SDK API.
	NativeOptions []any
}

// Server exposes SDK agents over A2A.
type Server struct {
	opts         Options
	store        taskstore.Store
	exposed      []schema.AgentInfo
	transports   []Transport
	capabilities a2a.AgentCapabilities
	pushStore    push.ConfigStore
	pushSender   push.Sender
	signer       *a2acrypto.Signer
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
	if opts.MaxBodyBytes < 0 {
		return nil, fmt.Errorf("a2a/server: MaxBodyBytes must not be negative")
	}
	store := opts.TaskStore
	if store == nil {
		if opts.Turns == nil {
			store = taskstore.NewInMemory(nil)
		} else {
			store = NewTurnTaskStore(opts.Turns)
		}
	}
	transports, err := normalizeTransports(opts.Transports)
	if err != nil {
		return nil, err
	}
	server := &Server{
		opts:       opts,
		store:      store,
		transports: transports,
		capabilities: a2a.AgentCapabilities{
			Streaming:         true,
			PushNotifications: opts.PushNotifications != nil,
		},
	}
	if opts.PushNotifications != nil {
		server.pushStore = push.NewInMemoryStore()
		server.pushSender = push.NewHTTPPushSender(&push.HTTPSenderConfig{
			Timeout:              opts.PushNotifications.Timeout,
			AllowPrivateNetworks: opts.PushNotifications.AllowPrivateNetworks,
			FailOnError:          opts.PushNotifications.FailOnError,
		})
	}
	if opts.Signing != nil {
		if opts.Signing.PrivateKey == nil {
			return nil, fmt.Errorf("a2a/server: signing: private key is required")
		}
		if err := validateSigningAlgorithm(opts.Signing.PrivateKey, opts.Signing.Algorithm); err != nil {
			return nil, fmt.Errorf("a2a/server: signing: %w", err)
		}
		signer, err := a2acrypto.NewSigner(a2acrypto.SignerConfig{
			PrivateKey: opts.Signing.PrivateKey,
			KeyID:      opts.Signing.KeyID,
			Algorithm:  opts.Signing.Algorithm,
			JWKSURL:    opts.Signing.JWKSURL,
		})
		if err != nil {
			return nil, fmt.Errorf("a2a/server: signing: %w", err)
		}
		server.signer = signer
	}
	for _, info := range opts.Agents {
		if info.Expose == nil {
			continue
		}
		server.exposed = append(server.exposed, info)
	}
	if len(server.exposed) == 0 {
		return nil, fmt.Errorf("a2a/server: no exposed agents (set AgentInfo.Expose)")
	}
	if err := validateExposedAgents(server.exposed, transports); err != nil {
		return nil, fmt.Errorf("a2a/server: %w", err)
	}
	exposedIDs := make(map[schema.AgentID]bool, len(server.exposed))
	for _, info := range server.exposed {
		exposedIDs[info.ID] = true
	}
	for id := range opts.ExtendedCards {
		if !exposedIDs[id] {
			return nil, fmt.Errorf("a2a/server: extended card for unexposed agent %q", id)
		}
	}
	return server, nil
}

// validateSigningAlgorithm rejects an explicit algorithm that cannot match the
// key type, which would otherwise produce cards that look signed but fail
// verification.
func validateSigningAlgorithm(key crypto.Signer, algorithm string) error {
	if key == nil {
		return fmt.Errorf("signing key is required")
	}
	if algorithm == "" {
		return nil
	}
	switch public := key.Public().(type) {
	case *ecdsa.PublicKey:
		want := map[int]string{256: "ES256", 384: "ES384", 521: "ES512"}[public.Curve.Params().BitSize]
		if want == "" || algorithm != want {
			return fmt.Errorf("algorithm %q does not match ECDSA curve %s", algorithm, public.Curve.Params().Name)
		}
	case *rsa.PublicKey:
		switch algorithm {
		case "RS256", "RS384", "RS512":
		default:
			return fmt.Errorf("algorithm %q does not match an RSA key", algorithm)
		}
	case ed25519.PublicKey:
		if algorithm != "EdDSA" {
			return fmt.Errorf("algorithm %q does not match an Ed25519 key", algorithm)
		}
	default:
		return fmt.Errorf("unsupported signing key type %T", public)
	}
	return nil
}

// normalizeTransports deduplicates and defaults the configured bindings.
func normalizeTransports(transports []Transport) ([]Transport, error) {
	if len(transports) == 0 {
		return []Transport{TransportJSONRPC}, nil
	}
	seen := make(map[Transport]bool, len(transports))
	out := make([]Transport, 0, len(transports))
	for _, transport := range transports {
		switch transport {
		case TransportJSONRPC, TransportREST:
		default:
			return nil, fmt.Errorf("a2a/server: unknown transport %q", transport)
		}
		if seen[transport] {
			return nil, fmt.Errorf("a2a/server: duplicate transport %q", transport)
		}
		seen[transport] = true
		out = append(out, transport)
	}
	return out, nil
}

// validateExposedAgents rejects inputs that would make Handler panic on a
// duplicate ServeMux registration or produce an unroutable card. A host may
// impose additional path restrictions (for example agentd reserves /api/config
// and /api/info for introspection).
func validateExposedAgents(agents []schema.AgentInfo, transports []Transport) error {
	ids := make(map[schema.AgentID]bool, len(agents))
	patterns := map[string]string{
		"/.well-known/agent-card.json": "the origin-root default card",
		DiscoveryPath:                  "the aggregate discovery endpoint",
	}
	restEnabled := false
	for _, transport := range transports {
		if transport == TransportREST {
			restEnabled = true
		}
	}

	for _, info := range agents {
		id := string(info.ID)
		if id == "" {
			return fmt.Errorf("agent id is required")
		}
		if ids[info.ID] {
			return fmt.Errorf("duplicate agent id %q", id)
		}
		ids[info.ID] = true
		if err := validateAgentID(id); err != nil {
			return err
		}
		endpoint := agentEndpointPath(info.ID)
		if prev, dup := patterns[endpoint]; dup {
			return fmt.Errorf("agent %q endpoint %q collides with %s", id, endpoint, prev)
		}
		patterns[endpoint] = "an agent endpoint"
	}

	for _, info := range agents {
		cardPath := effectiveCardPath(info)
		if err := validateCardPathPattern(cardPath); err != nil {
			return fmt.Errorf("agent %q: %w", info.ID, err)
		}
		if restEnabled {
			for _, other := range agents {
				restPrefix := agentEndpointPath(other.ID) + "rest"
				if strings.HasPrefix(cardPath, restPrefix+"/") {
					return fmt.Errorf("agent %q card path %q collides with the REST binding of agent %q", info.ID, cardPath, other.ID)
				}
			}
		}
		if prev, dup := patterns[cardPath]; dup {
			return fmt.Errorf("agent %q card path %q collides with %s", info.ID, cardPath, prev)
		}
		patterns[cardPath] = "an agent card path"
	}
	return nil
}

// validateAgentID requires an id that is safe as one URL path segment and in a
// ServeMux literal pattern.
func validateAgentID(id string) error {
	if id == "." || id == ".." {
		return fmt.Errorf("agent id %q is not path-safe", id)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`/\{}?#%`, r) {
			return fmt.Errorf("agent id %q is not path-safe", id)
		}
	}
	return nil
}

// validateCardPathPattern checks a custom card path's shape.
func validateCardPathPattern(cardPath string) error {
	if !strings.HasPrefix(cardPath, "/") {
		return fmt.Errorf("card path must start with /")
	}
	if cardPath == "/" || strings.HasSuffix(cardPath, "/") {
		return fmt.Errorf("card path %q must not be the root or end with /", cardPath)
	}
	if path.Clean(cardPath) != cardPath {
		return fmt.Errorf("card path %q is not a clean path", cardPath)
	}
	if strings.ContainsAny(cardPath, "{}`?#%\\ \t\r\n") {
		return fmt.Errorf("card path %q contains unsupported characters", cardPath)
	}
	return nil
}

// effectiveCardPath is the path an agent's card is served at.
func effectiveCardPath(info schema.AgentInfo) string {
	if info.Expose != nil && info.Expose.Path != "" {
		return info.Expose.Path
	}
	return agentCardPath(info.ID)
}

// agentEndpointPath is the JSON-RPC route for one agent.
func agentEndpointPath(id schema.AgentID) string {
	return "/agents/" + string(id) + "/"
}

// agentCardPath is the default card route for one agent.
func agentCardPath(id schema.AgentID) string {
	return agentEndpointPath(id) + ".well-known/agent-card.json"
}

// Handler returns the http.Handler serving the exposed agents' cards and one
// endpoint per configured transport and agent.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, info := range s.exposed {
		requestHandler := s.newRequestHandler(info)
		mux.Handle(s.cardPath(info), s.cardHandler(info))
		if s.hasTransport(TransportJSONRPC) {
			rpc := a2asrv.NewJSONRPCHandler(requestHandler, s.transportOptions()...)
			mux.Handle(s.endpointPath(info), s.secure(s.maxBody(withHTTPRequest(rpc))))
		}
		if s.hasTransport(TransportREST) {
			rest := a2asrv.NewRESTHandler(requestHandler, s.transportOptions()...)
			prefix := s.endpointPath(info) + "rest"
			mux.Handle(prefix+"/", http.StripPrefix(prefix, s.secure(s.maxBody(withHTTPRequest(rest)))))
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

// hasTransport reports whether the server serves the given binding.
func (s *Server) hasTransport(transport Transport) bool {
	for _, configured := range s.transports {
		if configured == transport {
			return true
		}
	}
	return false
}

// withHTTPRequest carries the HTTP request to card producers invoked by the
// transport handler.
func withHTTPRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(withRequest(r.Context(), r)))
	})
}

func (s *Server) endpointPath(info schema.AgentInfo) string {
	return agentEndpointPath(info.ID)
}

// maxBody caps an RPC request body to MaxBodyBytes (when set): a declared
// oversized Content-Length is rejected with 413 up front, and a chunked body is
// bounded by MaxBytesReader, so a request cannot buffer an unbounded inline part.
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
	return effectiveCardPath(info)
}

func (s *Server) cardHandler(info schema.AgentInfo) http.Handler {
	inner := a2asrv.NewAgentCardHandler(s.cardProducer(info, info.Expose))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(withRequest(r.Context(), r)))
	})
}

// cardProducer builds one agent's public card, signing it when configured.
func (s *Server) cardProducer(info schema.AgentInfo, expose *schema.AgentExpose) a2asrv.AgentCardProducer {
	producer := a2asrv.AgentCardProducerFn(func(ctx context.Context) (*a2a.AgentCard, error) {
		return buildCardWith(info, s.cardConfig(ctx, info, expose)), nil
	})
	return s.signedProducer(producer)
}

// extendedCardProducer builds the authenticated extended card for one agent.
func (s *Server) extendedCardProducer(info schema.AgentInfo, expose schema.AgentExpose) a2asrv.ExtendedAgentCardProducer {
	producer := s.cardProducer(info, &expose)
	return a2asrv.ExtendedAgentCardProducerFn(func(ctx context.Context, _ *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
		return producer.Card(ctx)
	})
}

// signedProducer wraps a card producer with the configured signer.
func (s *Server) signedProducer(producer a2asrv.AgentCardProducer) a2asrv.AgentCardProducer {
	if s.signer == nil {
		return producer
	}
	return a2asrv.NewSignedCardProducer(producer, func(context.Context) ([]*a2acrypto.Signer, error) {
		return []*a2acrypto.Signer{s.signer}, nil
	})
}

// agentCapabilities returns the declared capabilities for one agent.
func (s *Server) agentCapabilities(info schema.AgentInfo) a2a.AgentCapabilities {
	caps := s.capabilities
	_, caps.ExtendedAgentCard = s.opts.ExtendedCards[info.ID]
	return caps
}

// cardConfig resolves the card inputs for one agent.
func (s *Server) cardConfig(ctx context.Context, info schema.AgentInfo, expose *schema.AgentExpose) cardConfig {
	return cardConfig{
		expose:       expose,
		endpoint:     s.cardEndpoint(ctx, info),
		security:     s.opts.Security,
		capabilities: s.agentCapabilities(info),
		transports:   s.transports,
	}
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
// A card whose producer fails (for example, signing) is omitted and reported
// through OnError, rather than being served unsigned.
func (s *Server) Cards(r *http.Request) []*a2a.AgentCard {
	ctx := context.Background()
	if r != nil {
		ctx = withRequest(r.Context(), r)
	}
	cards := make([]*a2a.AgentCard, 0, len(s.exposed))
	for _, info := range s.exposed {
		card, err := s.cardProducer(info, info.Expose).Card(ctx)
		if err != nil {
			if s.opts.OnError != nil {
				s.opts.OnError(err)
			}
			continue
		}
		cards = append(cards, card)
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

// cardEndpoint resolves the absolute URL advertised in a card: BaseURL when set
// (it must already include PathPrefix), otherwise the request's own origin plus
// the mount prefix and the agent's endpoint path.
func (s *Server) cardEndpoint(ctx context.Context, info schema.AgentInfo) string {
	path := normalizePathPrefix(s.opts.PathPrefix) + s.endpointPath(info)
	if base := strings.TrimRight(s.opts.BaseURL, "/"); base != "" {
		return base + s.endpointPath(info)
	}
	if r, ok := requestFromContext(ctx); ok {
		return requestBaseURL(r) + path
	}
	return path
}

// normalizePathPrefix returns a leading-slash, no-trailing-slash prefix.
func normalizePathPrefix(prefix string) string {
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
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

// newRequestHandler builds the transport-agnostic handler for one agent.
func (s *Server) newRequestHandler(info schema.AgentInfo) a2asrv.RequestHandler {
	exec := newExecutor(executorConfig{
		factory:    s.opts.Factory,
		agentID:    string(info.ID),
		store:      s.store,
		workspaces: s.opts.Workspaces,
		artifacts:  s.opts.Artifacts,
	})
	caps := s.agentCapabilities(info)
	options := []a2asrv.RequestHandlerOption{
		a2asrv.WithTaskStore(s.store),
		a2asrv.WithCapabilityChecks(&caps),
	}
	if s.pushStore != nil && s.pushSender != nil {
		options = append(options, a2asrv.WithPushNotifications(s.pushStore, s.pushSender))
	}
	if expose, ok := s.opts.ExtendedCards[info.ID]; ok {
		options = append(options, a2asrv.WithExtendedAgentCardProducer(s.extendedCardProducer(info, expose)))
	}
	for _, option := range s.opts.NativeOptions {
		if typed, ok := option.(a2asrv.RequestHandlerOption); ok {
			options = append(options, typed)
		}
	}
	return a2asrv.NewHandler(exec, options...)
}

// transportOptions returns options shared by the JSON-RPC and REST handlers.
func (s *Server) transportOptions() []a2asrv.TransportOption {
	var options []a2asrv.TransportOption
	if s.opts.KeepAlive > 0 {
		options = append(options, a2asrv.WithTransportKeepAlive(s.opts.KeepAlive))
	}
	for _, option := range s.opts.NativeOptions {
		if typed, ok := option.(a2asrv.TransportOption); ok {
			options = append(options, typed)
		}
	}
	return options
}

// nowMillis returns the current Unix time in milliseconds.
func nowMillis() int64 {
	return time.Now().UnixMilli()
}
