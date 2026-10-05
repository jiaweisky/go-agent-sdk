package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/a2a/server"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// shutdownTimeout bounds graceful shutdown.
const shutdownTimeout = 10 * time.Second

// run builds the SDK client and A2A server from cfg and serves until ctx is
// canceled, then shuts down gracefully and closes the client.
func run(ctx context.Context, cfg config) error {
	keepAlive, err := cfg.keepAliveDuration()
	if err != nil {
		return err
	}

	client, err := agent.NewClient(&agent.ClientOptions{
		Root:             cfg.Root,
		WorkingDirectory: cfg.WorkingDirectory,
		ConfigDirs:       cfg.ConfigDirs,
		Model:            cfg.Model,
		Protocol:         schema.ProtocolID(cfg.Protocol),
		ModelInfo:        modelInfoProvider(cfg),
		Credentials:      credentialProvider(cfg),
		ContextLimit:     cfg.ContextLimit,
		OutputLimit:      cfg.OutputLimit,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	agents, err := exposeAgents(client, cfg.Agents, cfg.Cards)
	if err != nil {
		return err
	}

	factoryOptions := []agent.A2AFactoryOption{}
	if permissions := cfg.permissionRuleset(); len(permissions) > 0 {
		factoryOptions = append(factoryOptions, agent.WithInboundPermissions(permissions))
	}
	var security *server.Security
	if cfg.Auth != nil {
		security = &server.Security{Bearer: cfg.Auth.Bearer}
	}

	srv, err := server.New(server.Options{
		Factory:   agent.NewA2AFactory(client, factoryOptions...),
		Turns:     client.TurnStore(),
		Agents:    agents,
		BaseURL:   cfg.BaseURL,
		KeepAlive: keepAlive,
		Security:  security,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{Addr: cfg.Listen, Handler: srv.Handler()}
	fmt.Fprintf(os.Stderr, "a2aserve: listening on %s\n", cfg.Listen)
	fmt.Fprintf(os.Stderr, "a2aserve: card at %s/.well-known/agent-card.json\n", logBaseURL(cfg))

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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// exposeAgents marks each named existing agent exposed and returns its info.
// cards optionally overrides an agent's card name/description.
func exposeAgents(client *agent.Client, names []string, cards map[string]cardConfig) ([]schema.AgentInfo, error) {
	exposed := make([]schema.AgentInfo, 0, len(names))
	for _, name := range names {
		if info := client.Agents().Get(schema.AgentID(name)); info == nil {
			return nil, fmt.Errorf("unknown agent %q", name)
		}
		expose := &schema.AgentExpose{}
		if override, ok := cards[name]; ok {
			expose.Name = override.Name
			expose.Description = override.Description
		}
		if err := client.RegisterAgent(name, agent.AgentConfig{Expose: expose}); err != nil {
			return nil, fmt.Errorf("expose agent %q: %w", name, err)
		}
		exposed = append(exposed, *client.Agents().Get(schema.AgentID(name)))
	}
	return exposed, nil
}

// credentialProvider returns the configured credential. The server runs one
// fixed protocol, so the provider id (which equals the protocol id on this path)
// does not select: there is a single credential.
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

// modelInfoProvider supplies model metadata. Bedrock has no default endpoint
// (it is region-specific), so its base URL is derived from the AWS region; every
// other protocol uses its route's built-in endpoint.
func modelInfoProvider(cfg config) agent.ModelInfoProvider {
	baseURL := ""
	if cfg.Protocol == "bedrock-converse" && cfg.Credentials.AWS != nil {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", cfg.Credentials.AWS.Region)
	}
	return func(id string) (schema.ModelInfo, error) {
		return agent.NewModelInfo(schema.ProtocolID(cfg.Protocol), id, baseURL, schema.ModelLimit{})
	}
}

// logBaseURL derives the URL printed at startup.
func logBaseURL(cfg config) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return "http://" + cfg.Listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
