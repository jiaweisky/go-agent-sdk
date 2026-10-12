package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/isolation"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
	a2aserver "github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
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

// run builds one client per configured server, groups the servers by listener,
// and serves until ctx is canceled. Every client created is closed on return, so
// a mid-build failure leaks nothing.
func run(ctx context.Context, cfg config.Config) error {
	global, err := cfg.Catalog()
	if err != nil {
		return err
	}
	var clients []*agent.Client
	var teardowns []func(context.Context) error
	defer func() {
		for _, client := range clients {
			_ = client.Close()
		}
		for i := len(teardowns) - 1; i >= 0; i-- {
			_ = teardowns[i](context.Background())
		}
	}()

	runner := isolation.DefaultRunner()
	var built []*builtServer
	for _, s := range cfg.Servers {
		serverCatalog, err := global.Subset(s.Models, s.DefaultModel)
		if err != nil {
			return fmt.Errorf("server %q: %w", s.Name, err)
		}
		project, err := runner.Prepare(ctx, projectSpec(s), isolationSpec(s))
		if err != nil {
			return fmt.Errorf("server %q: %w", s.Name, err)
		}
		if project.Teardown != nil {
			teardowns = append(teardowns, project.Teardown)
		}
		s.WorkingDirectory = project.View
		uploader := mediaUploader(s)
		client, err := buildClient(s, serverCatalog, uploader)
		if err != nil {
			return fmt.Errorf("server %q: %w", s.Name, err)
		}
		clients = append(clients, client)
		b, err := buildServer(ctx, s, client, serverCatalog, uploader)
		if err != nil {
			return fmt.Errorf("server %q: %w", s.Name, err)
		}
		built = append(built, b)
	}

	byListen, order := groupByListen(built)

	// Bind every listener up front so a port conflict fails startup
	// synchronously (before serving) and the "listening" log reflects the real
	// address. Close them if a later bind fails.
	var targets []serveTarget
	defer func() {
		for _, target := range targets {
			_ = target.ln.Close()
		}
	}()
	for _, listen := range order {
		entries := byListen[listen]
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			return fmt.Errorf("listen %s: %w", listen, err)
		}
		fmt.Fprintf(os.Stderr, "agentd: listening on %s (%d server(s))\n", ln.Addr(), len(entries))
		if def := defaultA2AEntry(entries); def != nil {
			base := discoveryBaseURL(def.cfg, ln.Addr())
			fmt.Fprintf(os.Stderr, "agentd: a2a discovery: %s/.well-known/agent-card.json (default card)\n", base)
			fmt.Fprintf(os.Stderr, "agentd: a2a discovery: %s/.well-known/agents.json (all cards, codebuddy)\n", base)
		}
		for _, e := range entries {
			base := publicBaseURL(e.cfg, ln.Addr())
			for _, path := range e.cardPaths {
				fmt.Fprintf(os.Stderr, "agentd: a2a agent card: %s%s\n", base, path)
			}
		}
		targets = append(targets, serveTarget{
			srv:          &http.Server{Handler: newRouter(entries), ReadHeaderTimeout: 10 * time.Second},
			ln:           ln,
			tls:          entries[0].cfg.TLS,
			closeStreams: combineClose(entries),
		})
	}
	if cfg.Control != nil {
		manager := newProjectManager(ctx, global, cfg.Control)
		defer manager.Close()
		ln, err := net.Listen("tcp", cfg.Control.Listen)
		if err != nil {
			return fmt.Errorf("control listen %s: %w", cfg.Control.Listen, err)
		}
		fmt.Fprintf(os.Stderr, "agentd: control API on %s\n", ln.Addr())
		targets = append(targets, serveTarget{
			srv: &http.Server{
				Handler:           newControlHandler(manager, cfg.Control.Auth),
				ReadHeaderTimeout: 10 * time.Second,
			},
			ln: ln,
		})
	}
	return serveAll(ctx, targets)
}

// groupByListen groups entries by their listen address, returning the addresses
// in first-seen order (deterministic startup).
func groupByListen(built []*builtServer) (map[string][]*builtServer, []string) {
	byListen := map[string][]*builtServer{}
	order := []string{}
	for _, b := range built {
		if _, ok := byListen[b.cfg.Listen]; !ok {
			order = append(order, b.cfg.Listen)
		}
		byListen[b.cfg.Listen] = append(byListen[b.cfg.Listen], b)
	}
	return byListen, order
}

// defaultModelFor is a server's default model id, falling back to the catalog's
// first entry.
func defaultModelFor(s config.Server, catalog config.Catalog) string {
	if s.DefaultModel != "" {
		return s.DefaultModel
	}
	return catalog.DefaultID()
}

// a2aQuestions is an A2A server's question policy: "decline" by default, since a
// remote A2A client polls task state and cannot answer a parked question.
func a2aQuestions(s config.Server) string {
	if s.Questions != "" {
		return s.Questions
	}
	return "decline"
}

// httpQuestions is an HTTP server's question policy: "park" by default, since an
// HTTP client can answer over the API.
func httpQuestions(s config.Server) string {
	if s.Questions != "" {
		return s.Questions
	}
	return "park"
}

// publicBaseURL is the base a client uses to reach a server: the configured
// BaseURL when set, otherwise scheme + host (or the bound address) + mount.
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
	if s.Mount != "" {
		base += config.NormalizeMount(s.Mount)
	}
	return base
}

// defaultA2AEntry is the entry that owns the listener's origin-root card: the
// one marked Default, otherwise the first A2A entry. Nil when none is A2A.
func defaultA2AEntry(entries []*builtServer) *builtServer {
	var def *builtServer
	for _, e := range entries {
		if e.cfg.Kind != "a2a" {
			continue
		}
		if def == nil || e.cfg.Default {
			def = e
		}
	}
	return def
}

// discoveryBaseURL is the origin root (scheme + host, no mount) a client fetches
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
		Mode:         s.Isolation.Mode,
		BaseDir:      s.Isolation.BaseDir,
		BaseReadOnly: s.Isolation.BaseReadOnly,
		BaseRef:      s.Isolation.BaseRef,
		CowMode:      cowMode(s.Isolation.Cow),
	}
}

// cowMode reads the configured copy-on-write mode, defaulting to empty (auto).
func cowMode(cow *config.CowConfig) string {
	if cow == nil {
		return ""
	}
	return cow.Mode
}

// buildClient assembles one server's agent client from its catalog subset.
func buildClient(s config.Server, catalog config.Catalog, uploader mediaUploaderFunc) (*agent.Client, error) {
	mcpServers, err := config.MCPServers(s.MCP)
	if err != nil {
		return nil, err
	}
	references, err := config.ReferenceSources(s.References)
	if err != nil {
		return nil, err
	}
	process, err := resolveSandbox(s)
	if err != nil {
		return nil, err
	}
	return config.BuildClient(config.ClientParams{
		Root:             s.Root,
		WorkingDirectory: s.WorkingDirectory,
		ConfigDirs:       s.ConfigDirs,
		Model:            defaultModelFor(s, catalog),
		// Protocol is left empty: the model's wire protocol comes from the
		// catalog's ModelInfo, not from the server.
		Catalog:          catalog,
		MediaUploader:    uploader,
		MCP:              mcpServers,
		References:       references,
		SubagentDepth:    s.SubagentDepth,
		TitleAuto:        s.TitleAuto,
		DropErroredTurns: s.DropErroredTurns == nil || *s.DropErroredTurns,
		Process:          process,
	})
}

// resolveSandbox returns the server's command-execution seam. No sandbox (or
// "none") yields nil, the SDK default. When a named sandbox cannot be built,
// fail-closed (the default) refuses to start; fail-open warns and runs without
// it.
func resolveSandbox(s config.Server) (core.AppProcess, error) {
	if s.Sandbox == nil {
		return nil, nil
	}
	process, err := sandbox.Resolve(s.Sandbox.Process)
	if err == nil {
		return process, nil
	}
	if s.Sandbox.FailurePolicy == "fail-open" {
		fmt.Fprintf(os.Stderr, "agentd: server %q: sandbox %q unavailable (%v); running without it\n", s.Name, s.Sandbox.Process, err)
		return nil, nil
	}
	return nil, err
}

// buildServer mounts one server's transport adapter, dispatching on its kind.
func buildServer(ctx context.Context, s config.Server, client *agent.Client, catalog config.Catalog, uploader mediaUploaderFunc) (*builtServer, error) {
	infos, err := config.ModelInfos(catalog)
	if err != nil {
		return nil, err
	}
	keepAlive, err := config.KeepAliveDuration(s.KeepAlive)
	if err != nil {
		return nil, err
	}
	switch s.Kind {
	case "http":
		handler, closeStreams, err := buildHTTPHandler(ctx, s, client, infos, uploader, keepAlive, defaultModelFor(s, catalog))
		if err != nil {
			return nil, err
		}
		return &builtServer{cfg: s, handler: handler, closeStreams: closeStreams}, nil
	case "a2a":
		srv, err := buildA2AServer(s, client, infos, defaultModelFor(s, catalog), keepAlive)
		if err != nil {
			return nil, err
		}
		return &builtServer{cfg: s, handler: srv.Handler(), cards: srv.Cards, cardPaths: srv.CardPaths()}, nil
	default:
		return nil, fmt.Errorf("unknown kind %q", s.Kind)
	}
}

func buildHTTPHandler(ctx context.Context, s config.Server, client *agent.Client, infos []schema.ModelInfo, uploader mediaUploaderFunc, keepAlive time.Duration, defaultModel string) (http.Handler, func(), error) {
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
		InboundPermissions:  config.PermissionRuleset(s.InboundPermissions),
		InteractionTimeout:  timeout,
		InteractionFallback: s.InteractionFallback,
		Questions:           httpQuestions(s),
		SubscriberCapacity:  s.SubscriberCapacity,
		KeepAlive:           keepAlive,
		Public:              s.PublicRoutes,
	})
	if err != nil {
		return nil, nil, err
	}
	return srv.Handler(), func() { _ = srv.Close() }, nil
}

func buildA2AServer(s config.Server, client *agent.Client, infos []schema.ModelInfo, defaultModel string, keepAlive time.Duration) (*a2aserver.Server, error) {
	exposed, err := config.ExposeAgents(client, s.Agents, s.Cards, config.ModelModes(infos), defaultModel)
	if err != nil {
		return nil, err
	}
	factoryOpts := []agent.A2AFactoryOption{}
	if permissions := config.PermissionRuleset(s.InboundPermissions); len(permissions) > 0 {
		factoryOpts = append(factoryOpts, agent.WithInboundPermissions(permissions))
	}
	if s.InboundMedia != "" {
		factoryOpts = append(factoryOpts, agent.WithInboundMedia(s.InboundMedia))
	}
	factoryOpts = append(factoryOpts, agent.WithInboundQuestions(a2aQuestions(s)))
	var security *a2aserver.Security
	if s.Auth != nil {
		security = &a2aserver.Security{Bearer: s.Auth.Bearer}
	}
	return a2aserver.New(a2aserver.Options{
		Factory:   agent.NewA2AFactory(client, factoryOpts...),
		Turns:     client.TurnStore(),
		Agents:    exposed,
		BaseURL:   s.BaseURL,
		KeepAlive: keepAlive,
		Security:  security,
	})
}

// serveAll runs one serve per listener under one context and signal; a failed
// listener cancels the others so startup fails fast.
func serveAll(ctx context.Context, targets []serveTarget) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, len(targets))
	for _, target := range targets {
		go func(target serveTarget) {
			errs <- serve(ctx, target.srv, target.ln, target.tls, target.closeStreams)
		}(target)
	}
	var first error
	for range targets {
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

// mediaUploaderFunc adapts the configured uploader to the SDK's per-provider
// uploader signature.
type mediaUploaderFunc func(schema.ProviderID) schema.MediaUploader

func mediaUploader(s config.Server) mediaUploaderFunc {
	if s.MediaUpload == nil {
		return nil
	}
	uploader := s.MediaUpload.Uploader()
	return func(schema.ProviderID) schema.MediaUploader { return uploader }
}

// httpAuth maps a server's auth config to the HTTP server's auth options.
func httpAuth(s config.Server) *server.Auth {
	if s.Auth == nil {
		return nil
	}
	if s.Auth.Bearer != "" {
		return &server.Auth{Scheme: "bearer", Token: s.Auth.Bearer}
	}
	username := s.Auth.Username
	if username == "" {
		username = "opencode"
	}
	return &server.Auth{Scheme: "basic", Username: username, Password: s.Auth.Password}
}
