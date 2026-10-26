package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"

	agentdclient "github.com/jiaweisky/go-agent-sdk/cmd/agentd/client"
	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/isolation"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/media"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandboxpolicy"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/storage"
	a2aserver "github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/server"
)

// shutdownTimeout bounds graceful shutdown before connections are force-closed.
const shutdownTimeout = 10 * time.Second

// serve runs the HTTP server on an already-bound listener until ctx is canceled,
// then shuts down gracefully: it waits up to shutdownTimeout for in-flight
// requests and force-closes on timeout. beforeShutdown, when set, runs first so
// long-lived streams (SSE) end and Shutdown can complete. The caller closes the
// SDK client afterwards.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, tls *config.TLS, beforeShutdown func()) error {
	serveErr := make(chan error, 1)
	go func() {
		if tls != nil {
			serveErr <- srv.ServeTLS(ln, tls.CertFile, tls.KeyFile)
			return
		}
		serveErr <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		if beforeShutdown != nil {
			beforeShutdown()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			return err
		}
		return nil
	case err := <-serveErr:
		// The listener failed (e.g. the address is in use): release the server
		// too, so it is not left with live goroutines.
		if beforeShutdown != nil {
			beforeShutdown()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// serveAll runs one serve per listener under one context and signal; a failed
// listener cancels the others so startup fails fast.
func serveAll(ctx context.Context, serveTargets []serveTarget) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, len(serveTargets))
	for _, target := range serveTargets {
		go func(target serveTarget) {
			errs <- serve(ctx, target.srv, target.ln, target.tls, target.closeStreams)
		}(target)
	}
	var first error
	for range serveTargets {
		if err := <-errs; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	return first
}

// combineClose runs every entry's closeStreams hook.
func combineClose(entries []*builtServer) func() {
	return func() {
		for _, e := range entries {
			if e.closeStreams != nil {
				e.closeStreams()
			}
		}
	}
}

// builtServer is one served instance: its transport handler plus the hooks the
// router and shutdown need.
type builtServer struct {
	cfg          config.Server
	handler      http.Handler
	closeStreams func()
	cards        func(*http.Request) []*a2a.AgentCard // A2A only
	cardPaths    []string                             // A2A only: each agent's card path
}

// serveTarget is one bound listener plus the hook that stops its live streams
// (SSE) so shutdown can complete.
type serveTarget struct {
	srv          *http.Server
	ln           net.Listener
	tls          *config.TLS
	closeStreams func()
}

// mediaUploaderFunc adapts the configured uploader to the SDK's per-provider
// uploader signature.
type mediaUploaderFunc func(schema.ProviderID) schema.MediaUploader

// run builds one client per configured server, groups the servers by listener,
// and serves until ctx is canceled. Every client created is closed on return, so
// a mid-build failure leaks nothing.
func run(ctx context.Context, cfg config.Config, introspect *introspectDoc) error {
	registry, err := cfg.ModelRegistry()
	if err != nil {
		return err
	}

	var runtimes []builtRuntime
	defer func() {
		for _, rt := range runtimes {
			if rt.client != nil {
				_ = rt.client.Close()
			}
		}
		for i := len(runtimes) - 1; i >= 0; i-- {
			if runtimes[i].teardown == nil {
				continue
			}
			teardownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			_ = runtimes[i].teardown(teardownCtx)
			cancel()
		}
	}()

	runner := isolation.DefaultRunner()
	baseDirs := isolationBaseDirs(cfg.Servers)

	var builtServers []*builtServer
	for _, s := range cfg.Servers {
		rt, err := buildRuntime(ctx, s, cfg.Models, cfg.ProviderFileAPIs, registry, baseDirs, runner)
		runtimes = append(runtimes, rt)
		if err != nil {
			return err
		}
		builtServers = append(builtServers, rt.server)
	}

	serveTargets, err := bindServeTargets(builtServers, introspect)
	if err != nil {
		return err
	}
	defer func() { closeServeTargets(serveTargets) }()

	if cfg.Control != nil {
		target, closeManager, err := bindControlTarget(ctx, registry, cfg.Control, cfg.Models, cfg.ProviderFileAPIs)
		if err != nil {
			return err
		}
		defer closeManager()
		serveTargets = append(serveTargets, target)
	}
	return serveAll(ctx, serveTargets)
}

// builtRuntime is one fully prepared server and the resources it owns.
type builtRuntime struct {
	client   *agent.Client
	server   *builtServer
	teardown func(context.Context) error
}

// buildRuntime prepares one server's view, client and transport handler. On
// failure it closes anything it already created and still returns the project
// teardown so the caller can release the view.
func buildRuntime(
	ctx context.Context, s config.Server, models []config.ModelConfig,
	profiles map[string]*config.ProviderFileAPI, registry config.ModelRegistry,
	baseDirs []string, runner isolation.Runner,
) (builtRuntime, error) {
	serverRegistry, err := registry.Subset(s.Models, s.DefaultModel)
	if err != nil {
		return builtRuntime{}, fmt.Errorf("server %q: %w", s.Name, err)
	}

	source := s.WorkingDirectory
	project, err := runner.Prepare(ctx, projectSpec(s), isolationSpec(s))
	if err != nil {
		return builtRuntime{}, fmt.Errorf("server %q: %w", s.Name, err)
	}

	runtime := builtRuntime{teardown: project.Teardown}
	s.WorkingDirectory = project.View
	uploader, err := mediaUploaderFor(s, models, profiles)
	if err != nil {
		return runtime, fmt.Errorf("server %q: %w", s.Name, err)
	}
	fileHeaders := mediaFileHeadersFor(s, models, profiles)

	client, err := buildClient(ctx, s, serverRegistry, uploader, fileHeaders, isolationExcludes(source, baseDirs))
	if err != nil {
		return runtime, fmt.Errorf("server %q: %w", s.Name, err)
	}

	server, err := buildServer(ctx, s, client, serverRegistry, uploader)
	if err != nil {
		_ = client.Close()
		return runtime, fmt.Errorf("server %q: %w", s.Name, err)
	}

	runtime.client = client
	runtime.server = server
	return runtime, nil
}

// bindServeTargets binds every configured listener up front so a port conflict
// fails startup before serving. Already-bound listeners are closed on failure.
func bindServeTargets(builtServers []*builtServer, introspect *introspectDoc) ([]serveTarget, error) {
	byListen, order := groupByListen(builtServers)
	var serveTargets []serveTarget
	for _, listen := range order {
		entries := byListen[listen]

		ln, err := net.Listen("tcp", listen)
		if err != nil {
			closeServeTargets(serveTargets)
			return nil, fmt.Errorf("listen %s: %w", listen, err)
		}
		fmt.Fprintf(os.Stderr, "agentd: listening on %s (%d server(s))\n", ln.Addr(), len(entries))
		logDiscoveryEndpoints(entries, ln.Addr())
		serveTargets = append(serveTargets, serveTarget{
			srv:          &http.Server{Handler: newRouter(entries, introspect, ln.Addr()), ReadHeaderTimeout: 10 * time.Second},
			ln:           ln,
			tls:          entries[0].cfg.TLS,
			closeStreams: combineClose(entries),
		})
	}
	return serveTargets, nil
}

// logDiscoveryEndpoints reports the cards served by one listener.
func logDiscoveryEndpoints(entries []*builtServer, addr net.Addr) {
	if def := defaultA2AEntry(entries); def != nil {
		base := discoveryBaseURL(def.cfg, addr)
		fmt.Fprintf(os.Stderr, "agentd: a2a discovery: %s/.well-known/agent-card.json (default card)\n", base)
		fmt.Fprintf(os.Stderr, "agentd: a2a discovery: %s/.well-known/agents.json (all cards, codebuddy)\n", base)
	}

	for _, e := range entries {
		base := publicBaseURL(e.cfg, addr)
		for _, path := range e.cardPaths {
			fmt.Fprintf(os.Stderr, "agentd: a2a agent card: %s%s\n", base, path)
		}
	}
}

// bindControlTarget binds the optional control API and returns its cleanup.
func bindControlTarget(
	ctx context.Context, registry config.ModelRegistry, control *config.ControlConfig,
	models []config.ModelConfig, profiles map[string]*config.ProviderFileAPI,
) (serveTarget, func(), error) {
	manager := newProjectManager(ctx, registry, control, models, profiles)
	ln, err := net.Listen("tcp", control.Listen)
	if err != nil {
		manager.Close()
		return serveTarget{}, nil, fmt.Errorf("control listen %s: %w", control.Listen, err)
	}
	fmt.Fprintf(os.Stderr, "agentd: control API on %s\n", ln.Addr())
	return serveTarget{
		srv: &http.Server{
			Handler:           newControlHandler(manager, control.Auth),
			ReadHeaderTimeout: 10 * time.Second,
		},
		ln: ln,
	}, manager.Close, nil
}

// closeServeTargets closes every listener that has been bound.
func closeServeTargets(serveTargets []serveTarget) {
	for _, target := range serveTargets {
		_ = target.ln.Close()
	}
}

// groupByListen groups entries by their normalized listen address, returning the
// addresses in first-seen order (deterministic startup).
func groupByListen(builtServers []*builtServer) (map[string][]*builtServer, []string) {
	byListen := map[string][]*builtServer{}
	order := []string{}

	for _, b := range builtServers {
		listen := config.NormalizeListenAddress(b.cfg.Listen)
		if _, ok := byListen[listen]; !ok {
			order = append(order, listen)
		}
		byListen[listen] = append(byListen[listen], b)
	}
	return byListen, order
}

// defaultModelFor is a server's default model id, falling back to the registry's
// first entry.
func defaultModelFor(s config.Server, registry config.ModelRegistry) string {
	if s.DefaultModel != "" {
		return s.DefaultModel
	}
	return registry.DefaultID()
}

// a2aQuestions is an A2A server's question policy: "decline" by default, since a
// remote A2A client polls task state and cannot answer a parked question.
func a2aQuestions(s config.Server) string {
	if s.Questions != "" {
		return string(s.Questions)
	}
	return string(config.QuestionsDecline)
}

// httpQuestions is an HTTP server's question policy: "park" by default, since an
// HTTP client can answer over the API.
func httpQuestions(s config.Server) string {
	if s.Questions != "" {
		return string(s.Questions)
	}
	return string(config.QuestionsPark)
}

// publicBaseURL is the base a client uses to reach a server: the configured
// BaseURL when set, otherwise scheme + host (or the bound address) + pathPrefix.
func publicBaseURL(s config.Server, addr net.Addr) string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	scheme := "http"
	if s.TLS != nil {
		scheme = "https"
	}
	host := s.Host
	if host == "" {
		host = addr.String()
	}
	base := scheme + "://" + host
	if s.PathPrefix != "" {
		base += config.NormalizePathPrefix(s.PathPrefix)
	}
	return base
}

// defaultA2AEntry is the entry that owns the listener's origin-root card: the
// one marked Default, otherwise the first A2A entry. Nil when none is A2A.
func defaultA2AEntry(entries []*builtServer) *builtServer {
	var def *builtServer
	for _, e := range entries {
		if e.cfg.Kind != config.KindA2A {
			continue
		}
		if def == nil || e.cfg.Default {
			def = e
		}
	}
	return def
}

// discoveryBaseURL is the origin root (scheme + host, no pathPrefix) a client fetches
// the well-known discovery endpoints from.
func discoveryBaseURL(s config.Server, addr net.Addr) string {
	if s.BaseURL != "" {
		if u, err := url.Parse(s.BaseURL); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	scheme := "http"
	if s.TLS != nil {
		scheme = "https"
	}
	host := s.Host
	if host == "" {
		host = addr.String()
	}
	return scheme + "://" + host
}

// projectSpec maps a server to the project it serves.
func projectSpec(s config.Server) isolation.ProjectSpec {
	return isolation.ProjectSpec{
		ProjectID:        s.Name,
		Root:             s.Root,
		WorkingDirectory: s.WorkingDirectory,
		ConfigDirs:       s.ConfigDirs,
	}
}

// isolationSpec maps a server's isolation config to a runner spec. A nil config
// means the default: no isolation (share the working directory).
func isolationSpec(s config.Server) isolation.IsolationSpec {
	if s.Isolation == nil {
		return isolation.IsolationSpec{}
	}
	return isolation.IsolationSpec{
		Mode:    string(s.Isolation.Mode),
		BaseDir: s.Isolation.BaseDir,
		BaseRef: s.Isolation.BaseRef,
		CowMode: string(cowMode(s.Isolation.Cow)),
	}
}

// cowMode reads the configured copy-on-write mode, defaulting to empty (auto).
func cowMode(cow *config.CowConfig) config.CowMode {
	if cow == nil {
		return ""
	}
	return cow.Mode
}

// buildClient assembles one server's agent client from its registry subset.
func buildClient(
	ctx context.Context, s config.Server, registry config.ModelRegistry, uploader mediaUploaderFunc,
	fileHeaders map[schema.ProviderID]map[string]string, excludes []string,
) (*agent.Client, error) {
	mcpServers, err := agentdclient.MCPConfigs(s.MCP)
	if err != nil {
		return nil, err
	}

	references, err := agentdclient.ReferenceSources(s.References)
	if err != nil {
		return nil, err
	}

	process, policy, err := resolveSandbox(s)
	if err != nil {
		return nil, err
	}

	readGuard, writeGuard := sandboxGuards(s, policy)

	var urlHost *config.StorageConfig
	var delivery *config.MediaDeliveryConfig
	if s.Media != nil {
		delivery = s.Media.Delivery
		if s.Media.Uploads != nil {
			urlHost = s.Media.Uploads.URLHost
		}
	}
	store, err := storage.New(urlHost)
	if err != nil {
		return nil, fmt.Errorf("server %q: media.uploads.urlHost: %w", s.Name, err)
	}

	return agentdclient.NewContext(ctx, agentdclient.Params{
		Root:             s.Root,
		WorkingDirectory: s.WorkingDirectory,
		ConfigDirs:       s.ConfigDirs,
		Model:            defaultModelFor(s, registry),
		ModelRegistry:    registry,
		MediaUploader:    uploader,
		MediaFileHeaders: fileHeaders,
		Media:            media.DeliveryPolicy(delivery, s.Root),
		MediaTranscode:   media.FFmpegTranscoder(),
		Store:            store,
		MCP:              mcpServers,
		References:       references,
		Peers:            s.Peers,
		SubagentDepth:    s.SubagentDepth,
		TitleAuto:        s.TitleAuto,
		DropErroredTurns: s.DropErroredTurns == nil || *s.DropErroredTurns,
		Process:          process,
		FileReadGuard:    readGuard,
		FileWriteGuard:   writeGuard,
		SearchExcludes:   excludes,
	})
}

// isolationBaseDirs collects the configured isolation base directories.
func isolationBaseDirs(servers []config.Server) []string {
	var dirs []string
	seen := map[string]struct{}{}

	for _, s := range servers {
		base := baseDirOf(s.Isolation)
		if base == "" {
			continue
		}
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}
		dirs = append(dirs, base)
	}
	return dirs
}

// baseDirOf returns an isolation config's base directory ("" when unset).
func baseDirOf(iso *config.IsolationConfig) string {
	if iso == nil {
		return ""
	}
	return iso.BaseDir
}

// isolationExcludes returns ripgrep globs (relative to source) that hide every
// base directory sitting inside the server's working directory, so the agent
// does not read an isolation view back as project content.
func isolationExcludes(source string, baseDirs []string) []string {
	var excludes []string
	for _, base := range baseDirs {
		if rel, ok := insidePath(source, base); ok {
			excludes = append(excludes, rel+"/**")
		}
	}
	return excludes
}

// insidePath reports whether path is inside root, returning its slash-separated
// relative path when it is. Equal or outside paths return false.
func insidePath(root, path string) (string, bool) {
	if root == "" || path == "" {
		return "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// sandboxGuards builds the in-process file guards the policy requires. The same
// guard enforces reads and writes, so both paths share one policy.
func sandboxGuards(s config.Server, policy sandbox.Policy) (core.FileReadGuard, core.FileWriteGuard) {
	if s.Sandbox == nil || !s.Sandbox.Enabled() {
		return nil, nil
	}
	guard := sandbox.NewPathGuard(policy)

	var readGuard core.FileReadGuard
	var writeGuard core.FileWriteGuard
	if policy.ReadMode == sandbox.FSModeAllow {
		readGuard = guard
	}
	if policy.WriteMode == sandbox.FSModeAllow {
		writeGuard = guard
	}
	return readGuard, writeGuard
}

// resolveSandbox returns the server's command-execution seam and its resolved
// policy. No sandbox (or "none") yields a nil process, the SDK default; an
// explicitly disabled sandbox is reported so it is never a silent opt-out. When
// a named sandbox cannot be built, fail-closed (the default) refuses to start;
// fail-open warns and runs without it (the in-process guards still apply).
func resolveSandbox(s config.Server) (core.AppProcess, sandbox.Policy, error) {
	policy := sandboxpolicy.ForServer(s.Sandbox, s.WorkingDirectory)
	if s.Root != "" {
		policy.TempRoot = filepath.Join(s.Root, "data", "sandbox-tmp")
		// Drop per-run temp left by a crashed previous process. The server owns
		// this root, so no live instance shares the directory.
		_ = os.RemoveAll(policy.TempRoot)
	}

	if s.Sandbox == nil {
		fmt.Fprintf(os.Stderr,
			"agentd: server %q: sandbox not configured; commands run unsandboxed "+
				"(permission and workspace policy still apply)\n", s.Name)
		return nil, policy, nil
	}

	if !s.Sandbox.Enabled() {
		fmt.Fprintf(os.Stderr,
			"agentd: server %q: sandbox disabled (wrapper %q, profile %q); "+
				"commands and in-process file tools are not confined\n",
			s.Name, s.Sandbox.Wrapper, s.Sandbox.Profile)
		return nil, policy, nil
	}

	process, err := sandbox.Resolve(string(s.Sandbox.Wrapper), policy)
	if err == nil {
		return process, policy, nil
	}

	if s.Sandbox.FailurePolicy == config.FailurePolicyFailOpen {
		fmt.Fprintf(os.Stderr,
			"agentd: server %q: sandbox %q unavailable (%v); continuing without the process wrapper "+
				"(in-process file guards still apply)\n", s.Name, s.Sandbox.Wrapper, err)
		return nil, policy, nil
	}
	return nil, policy, err
}

// buildServer builds one server's transport adapter, dispatching on its kind.
func buildServer(
	ctx context.Context, s config.Server, client *agent.Client,
	registry config.ModelRegistry, uploader mediaUploaderFunc,
) (*builtServer, error) {
	infos, err := agentdclient.ModelInfos(registry)
	if err != nil {
		return nil, err
	}

	keepAlive, err := config.KeepAliveDuration(s.KeepAlive)
	if err != nil {
		return nil, err
	}

	switch s.Kind {
	case config.KindHTTP:
		handler, closeStreams, err := buildHTTPHandler(
			ctx, s, client, infos, uploader, keepAlive, defaultModelFor(s, registry),
		)
		if err != nil {
			return nil, err
		}
		return &builtServer{cfg: s, handler: handler, closeStreams: closeStreams}, nil
	case config.KindA2A:
		srv, err := buildA2AServer(s, client, infos, defaultModelFor(s, registry), keepAlive)
		if err != nil {
			return nil, err
		}
		return &builtServer{cfg: s, handler: srv.Handler(), cards: srv.Cards, cardPaths: srv.CardPaths()}, nil
	default:
		return nil, fmt.Errorf("unknown kind %q", s.Kind)
	}
}

// buildHTTPHandler builds one HTTP/SSE transport and its stream-close hook.
func buildHTTPHandler(
	ctx context.Context, s config.Server, client *agent.Client, infos []schema.ModelInfo,
	uploader mediaUploaderFunc, keepAlive time.Duration, defaultModel string,
) (http.Handler, func(), error) {
	timeout, err := config.InteractionTimeout(s.InteractionTimeout)
	if err != nil {
		return nil, nil, err
	}

	srv, err := server.New(ctx, client, server.Options{
		Auth:                httpAuth(s),
		Agents:              s.Agents,
		Models:              infos,
		DefaultModel:        defaultModel,
		MediaUploader:       uploader,
		CORS:                s.CORS,
		InboundPermissions:  httpInboundPermissions(s),
		InteractionTimeout:  timeout,
		InteractionFallback: s.InteractionFallback,
		Questions:           httpQuestions(s),
		MaxBodyBytes:        maxBodyBytes(s),
		SubscriberCapacity:  s.SubscriberCapacity,
		KeepAlive:           keepAlive,
		Public:              s.PublicRoutes,
	})
	if err != nil {
		return nil, nil, err
	}
	return srv.Handler(), func() { _ = srv.Close() }, nil
}

// buildA2AServer builds one A2A transport with its exposed agents and security.
func buildA2AServer(
	s config.Server, client *agent.Client, infos []schema.ModelInfo,
	defaultModel string, keepAlive time.Duration,
) (*a2aserver.Server, error) {
	exposed, err := agentdclient.ExposeAgents(client, s.Agents, s.Cards, agentdclient.ModelModes(infos), defaultModel)
	if err != nil {
		return nil, err
	}

	factoryOpts := []agent.A2AFactoryOption{}
	if permissions := config.PermissionRuleset(s.InboundPermissions); len(permissions) > 0 {
		factoryOpts = append(factoryOpts, agent.WithInboundPermissions(permissions))
	}
	if s.InboundMedia != "" {
		factoryOpts = append(factoryOpts, agent.WithInboundMedia(string(s.InboundMedia)))
	}
	factoryOpts = append(factoryOpts, agent.WithInboundQuestions(a2aQuestions(s)))

	var security *a2aserver.Security
	if s.Auth != nil {
		security = &a2aserver.Security{Bearer: s.Auth.Token}
	}

	pushNotifications, err := a2aPushOptions(s.PushNotifications)
	if err != nil {
		return nil, fmt.Errorf("server %q: %w", s.Name, err)
	}
	signing, err := a2aSigningOptions(s.Signing)
	if err != nil {
		return nil, fmt.Errorf("server %q: %w", s.Name, err)
	}

	return a2aserver.New(a2aserver.Options{
		Factory:           agent.NewA2AFactory(client, factoryOpts...),
		Turns:             client.TurnStore(),
		Agents:            exposed,
		BaseURL:           s.BaseURL,
		PathPrefix:        config.NormalizePathPrefix(s.PathPrefix),
		KeepAlive:         keepAlive,
		Security:          security,
		MaxBodyBytes:      maxBodyBytes(s),
		Transports:        a2aTransports(s.Transports),
		PushNotifications: pushNotifications,
		ExtendedCards:     a2aExtendedCards(s.Cards, exposed),
		Signing:           signing,
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "agentd: server %q: card discovery: %v\n", s.Name, err)
		},
	})
}

// mediaUploaderFor builds the per-server providerID -> uploader map from the
// explicit model references. A model without a reference is absent.
func mediaUploaderFor(
	s config.Server, models []config.ModelConfig,
	profiles map[string]*config.ProviderFileAPI,
) (mediaUploaderFunc, error) {
	byID := make(map[string]config.ModelConfig, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	ids := s.Models
	if len(ids) == 0 {
		ids = make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
	}

	uploaders := map[schema.ProviderID]schema.MediaUploader{}
	profilesByProvider := map[schema.ProviderID]string{}
	for _, id := range ids {
		entry, ok := byID[id]
		if !ok || entry.ProviderFileAPI == "" {
			continue
		}
		if entry.ProviderID == "" {
			return nil, fmt.Errorf("model %q uses providerFileAPI %q but has no providerID", entry.ID, entry.ProviderFileAPI)
		}

		profile := profiles[entry.ProviderFileAPI]
		if profile == nil {
			return nil, fmt.Errorf("model %q references unknown providerFileAPI %q", entry.ID, entry.ProviderFileAPI)
		}

		provider := schema.ProviderID(entry.ProviderID)
		if prev, ok := profilesByProvider[provider]; ok && prev != entry.ProviderFileAPI {
			return nil, fmt.Errorf("providerID %q is used with providerFileAPI %q and %q; assign a distinct providerID", entry.ProviderID, prev, entry.ProviderFileAPI)
		}
		if !providerFileUsable(entry, profile) {
			continue
		}

		uploader, err := media.NewUploader(entry.ProviderFileAPI, profile)
		if err != nil {
			return nil, err
		}
		profilesByProvider[provider] = entry.ProviderFileAPI
		uploaders[provider] = uploader
	}

	if len(uploaders) == 0 {
		return nil, nil
	}
	return func(provider schema.ProviderID) schema.MediaUploader { return uploaders[provider] }, nil
}

// providerFileUsable reports whether this model can actually consume a
// provider file reference: the protocol carries the source, the model declares
// it (when forms are present), and the profile shares a file MIME with the
// protocol/model.
func providerFileUsable(entry config.ModelConfig, profile *config.ProviderFileAPI) bool {
	if entry.Media != nil && len(entry.Media.Forms) > 0 {
		hasFile := false
		for _, forms := range entry.Media.Forms {
			for _, form := range forms {
				if form == string(protocols.MediaSourceFile) {
					hasFile = true
					break
				}
			}
		}
		if !hasFile {
			return false
		}
	}

	protocol, ok := protocols.LookupProtocol(llmschema.ProtocolID(entry.Protocol))
	if !ok || !protocol.Media.AllowsSource(protocols.MediaSourceFile) {
		return false
	}

	allowed := profile.MIME.Allowed
	if sourceSet, ok := protocol.Media.SourceMIMEs[protocols.MediaSourceFile]; ok && len(sourceSet) > 0 {
		filtered := allowed[:0:0]
		for _, mime := range allowed {
			if _, ok := sourceSet[mime]; ok {
				filtered = append(filtered, mime)
			}
		}
		allowed = filtered
	}

	if len(entry.InputMIMEs) > 0 {
		filtered := allowed[:0:0]
		for _, mime := range allowed {
			for _, modelMIME := range entry.InputMIMEs {
				if mime == modelMIME {
					filtered = append(filtered, mime)
					break
				}
			}
		}
		allowed = filtered
	}

	return len(allowed) > 0
}

// mediaFileHeadersFor maps each selected providerID to its profile's model
// headers. The headers are applied only when a request carries a file reference.
func mediaFileHeadersFor(s config.Server, models []config.ModelConfig, profiles map[string]*config.ProviderFileAPI) map[schema.ProviderID]map[string]string {
	byID := make(map[string]config.ModelConfig, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	ids := s.Models
	if len(ids) == 0 {
		ids = make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
	}

	headers := map[schema.ProviderID]map[string]string{}
	for _, id := range ids {
		entry, ok := byID[id]
		if !ok || entry.ProviderFileAPI == "" || entry.ProviderID == "" {
			continue
		}
		profile := profiles[entry.ProviderFileAPI]
		if profile == nil || len(profile.ModelHeaders) == 0 {
			continue
		}
		provider := schema.ProviderID(entry.ProviderID)
		if headers[provider] == nil {
			headers[provider] = map[string]string{}
		}
		for name, value := range profile.ModelHeaders {
			headers[provider][name] = value
		}
	}

	if len(headers) == 0 {
		return nil
	}
	return headers
}

// httpInboundPermissions extends the operator's inbound ruleset with an
// external_directory read allow for the delivery policy's spool dir, so the
// agent's tools can reach a spooled attachment. When the operator set no rules,
// the restrictive default is kept (the allow is appended to it).
func httpInboundPermissions(s config.Server) schema.PermissionRuleset {
	perms := config.PermissionRuleset(s.InboundPermissions)
	spoolDir := ""
	if s.Media != nil && s.Media.Delivery != nil && s.Media.Delivery.Spool != nil && s.Media.Delivery.Spool.Dir != "" {
		spoolDir = strings.ReplaceAll(s.Media.Delivery.Spool.Dir, "{Root}", s.Root)
	}
	if spoolDir == "" {
		return perms
	}
	if len(perms) == 0 {
		perms = agent.DefaultInboundPermissions()
	}
	return append(perms, schema.PermissionRule{
		Action: "external_directory", Resource: filepath.Join(spoolDir, "*"), Effect: schema.PermissionEffectAllow,
	})
}

// defaultMaxBodyBytes bounds an HTTP request body when the server sets none.
const defaultMaxBodyBytes = 128 << 20

// maxBodyBytes resolves the server's request-body cap (0 = the default).
func maxBodyBytes(s config.Server) int64 {
	if s.MaxBodyBytes > 0 {
		return s.MaxBodyBytes
	}
	return defaultMaxBodyBytes
}

// httpAuth maps a server's auth config to the HTTP server's auth options.
func httpAuth(s config.Server) *server.Auth {
	if s.Auth == nil {
		return nil
	}
	return &server.Auth{Token: s.Auth.Token}
}
