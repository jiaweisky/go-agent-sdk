package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	"github.com/jiaweisky/go-agent-sdk/schema"
	"github.com/jiaweisky/go-agent-sdk/server"
)

const shutdownTimeout = 10 * time.Second

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
	srv, err := buildServer(client, cfg, keepAlive, timeout)
	if err != nil {
		_ = client.Close()
		return err
	}
	return serve(ctx, cfg, srv, client)
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
	return agent.NewClient(clientOpts)
}

// buildServer assembles the HTTP server options from cfg.
func buildServer(client *agent.Client, cfg config, keepAlive, interactionTimeout time.Duration) (*server.Server, error) {
	var auth *server.Auth
	if cfg.Auth != nil {
		auth = authFromConfig(cfg.Auth)
	}
	return server.New(client, server.Options{
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
func serve(ctx context.Context, cfg config, srv *server.Server, client *agent.Client) error {
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "agentserve: listening on %s\n", cfg.Listen)

	serveErr := make(chan error, 1)
	go func() {
		if cfg.TLS != nil {
			serveErr <- httpServer.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			return
		}
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = srv.Close() // cancel SSE streams so Shutdown can complete
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownCtx)
		_ = client.Close()
		return shutdownErr
	case err := <-serveErr:
		_ = srv.Close()
		_ = client.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func authFromConfig(auth *authConfig) *server.Auth {
	if auth.Bearer != "" {
		return &server.Auth{Scheme: "bearer", Token: auth.Bearer}
	}
	return &server.Auth{Scheme: "basic", Username: auth.Username, Password: auth.Password}
}
