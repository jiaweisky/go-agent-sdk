package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	"github.com/jiaweisky/go-agent-sdk/schema"
	"github.com/jiaweisky/go-agent-sdk/server"
)

// run builds the SDK client and HTTP server from cfg, serves until ctx is
// canceled, then shuts down (Server.Close -> http.Shutdown -> Client.Close).
func run(ctx context.Context, cfg config) error {
	keepAlive, err := cfg.keepAliveDuration()
	if err != nil {
		return err
	}
	timeout, err := cfg.interactionTimeout()
	if err != nil {
		return err
	}

	client, err := buildClient(cfg)
	if err != nil {
		return err
	}
	srv, err := buildServer(ctx, client, cfg, keepAlive, timeout)
	if err != nil {
		_ = client.Close()
		return err
	}
	defer client.Close()
	return serve(ctx, cfg, srv)
}

// buildClient assembles the SDK client from the model, credential and limit
// settings.
func buildClient(cfg config) (*agent.Client, error) {
	clientOpts := &agent.ClientOptions{
		Root:             cfg.Root,
		WorkingDirectory: cfg.WorkingDirectory,
		ConfigDirs:       cfg.ConfigDirs,
		Model:            cfg.Model,
		Protocol:         schema.ProtocolID(cfg.Protocol),
		ModelInfo:        serveconfig.ModelInfoProvider(cfg.Protocol, cfg.ModelBaseURL, cfg.Credentials.AWS),
		Credentials:      serveconfig.CredentialProvider(cfg.Credentials),
		ContextLimit:     cfg.ContextLimit,
		OutputLimit:      cfg.OutputLimit,
	}
	if cfg.SubagentDepth > 0 {
		clientOpts.SubagentDepth = cfg.SubagentDepth
	}
	if cfg.TitleAuto {
		auto := true
		clientOpts.Title = &agent.TitleConfig{Auto: &auto}
	}
	mcpServers, err := cfg.mcpServers()
	if err != nil {
		return nil, err
	}
	clientOpts.MCP = mcpServers
	references, err := cfg.referenceSources()
	if err != nil {
		return nil, err
	}
	clientOpts.References = references
	return agent.NewClient(clientOpts)
}

// buildServer assembles the HTTP server options from cfg.
func buildServer(ctx context.Context, client *agent.Client, cfg config, keepAlive, interactionTimeout time.Duration) (*server.Server, error) {
	var auth *server.Auth
	if cfg.Auth != nil {
		auth = authFromConfig(cfg.Auth)
	}
	return server.New(ctx, client, server.Options{
		Auth:                auth,
		Agents:              cfg.Agents,
		CORS:                cfg.CORS,
		InboundPermissions:  serveconfig.PermissionRuleset(cfg.InboundPermissions),
		InteractionTimeout:  interactionTimeout,
		InteractionFallback: cfg.InteractionFallback,
		SubscriberCapacity:  cfg.SubscriberCapacity,
		KeepAlive:           keepAlive,
		Public:              cfg.PublicRoutes,
	})
}

// serve runs the HTTP server until ctx is canceled, then closes the server and
// the client.
func serve(ctx context.Context, cfg config, srv *server.Server) error {
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "agentserve: listening on %s\n", cfg.Listen)
	// Server.Close cancels the SSE streams so Shutdown can complete.
	return serveconfig.Serve(ctx, httpServer, cfg.TLS, func() { _ = srv.Close() })
}

func authFromConfig(auth *authConfig) *server.Auth {
	if auth.Bearer != "" {
		return &server.Auth{Scheme: "bearer", Token: auth.Bearer}
	}
	username := auth.Username
	if username == "" {
		username = "opencode"
	}
	return &server.Auth{Scheme: "basic", Username: username, Password: auth.Password}
}
