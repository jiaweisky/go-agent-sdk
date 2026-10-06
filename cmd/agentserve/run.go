package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
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

	clientOpts := &agent.ClientOptions{
		Root:             cfg.Root,
		WorkingDirectory: cfg.WorkingDirectory,
		ConfigDirs:       cfg.ConfigDirs,
		Model:            cfg.Model,
		Protocol:         schema.ProtocolID(cfg.Protocol),
		ModelInfo:        modelInfoProvider(cfg),
		Credentials:      credentialProvider(cfg),
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

	client, err := agent.NewClient(clientOpts)
	if err != nil {
		return err
	}

	var auth *server.Auth
	if cfg.Auth != nil {
		auth = authFromConfig(cfg.Auth)
	}
	srv, err := server.New(client, server.Options{
		Auth:                auth,
		BaseURL:             cfg.BaseURL,
		Agents:              cfg.Agents,
		CORS:                cfg.CORS,
		InboundPermissions:  cfg.permissionRuleset(),
		InteractionTimeout:  timeout,
		InteractionFallback: cfg.InteractionFallback,
		SubscriberCapacity:  cfg.SubscriberCapacity,
		KeepAlive:           keepAlive,
		Public:              cfg.PublicRoutes,
	})
	if err != nil {
		_ = client.Close()
		return err
	}

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

func credentialProvider(cfg config) agent.CredentialProvider {
	return func(schema.ProviderID) (schema.Credential, error) {
		credential := schema.Credential{APIKey: cfg.Credentials.APIKey}
		if aws := cfg.Credentials.AWS; aws != nil {
			credential.AWS = &schema.AWSCredentials{
				Region:          aws.Region,
				AccessKeyID:     aws.AccessKeyID,
				SecretAccessKey: aws.SecretAccessKey,
				SessionToken:    aws.SessionToken,
			}
		}
		return credential, nil
	}
}

func modelInfoProvider(cfg config) agent.ModelInfoProvider {
	baseURL := cfg.ModelBaseURL
	if baseURL == "" && cfg.Protocol == "bedrock-converse" && cfg.Credentials.AWS != nil {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", cfg.Credentials.AWS.Region)
	}
	return func(id string) (schema.ModelInfo, error) {
		return agent.NewModelInfo(schema.ProtocolID(cfg.Protocol), id, baseURL, schema.ModelLimit{})
	}
}
