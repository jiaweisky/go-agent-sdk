package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/a2a/server"
	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	"github.com/jiaweisky/go-agent-sdk/schema"
	"golang.org/x/text/language"
)

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
		ModelInfo:        serveconfig.ModelInfoProvider(cfg.Protocol, cfg.ModelBaseURL, cfg.Credentials.AWS, cfg.InputModalities),
		Credentials:      serveconfig.CredentialProvider(cfg.Credentials),
		ContextLimit:     cfg.ContextLimit,
		OutputLimit:      cfg.OutputLimit,
		DropErroredTurns: cfg.DropErroredTurns,
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
	if permissions := serveconfig.PermissionRuleset(cfg.InboundPermissions); len(permissions) > 0 {
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
	fmt.Fprintf(os.Stderr, "a2aserve: agent cards at %s%s\n", logBaseURL(cfg), server.DiscoveryPath)

	return serveconfig.Serve(ctx, httpServer, cfg.TLS, nil)
}

// exposeAgents marks each named existing agent exposed and returns its info.
// cards optionally overrides an agent's card; every skill id is validated
// against the agent's real capabilities.
func exposeAgents(client *agent.Client, names []string, cards map[string]cardConfig) ([]schema.AgentInfo, error) {
	exposed := make([]schema.AgentInfo, 0, len(names))
	for _, name := range names {
		if info := client.Agents().Get(schema.AgentID(name)); info == nil {
			return nil, fmt.Errorf("unknown agent %q", name)
		}
		expose := &schema.AgentExpose{}
		if override, ok := cards[name]; ok {
			if err := validateSkills(client, name, override.Skills); err != nil {
				return nil, err
			}
			applyCard(expose, override)
		}
		if err := client.RegisterAgent(name, agent.AgentConfig{Expose: expose}); err != nil {
			return nil, fmt.Errorf("expose agent %q: %w", name, err)
		}
		exposed = append(exposed, *client.Agents().Get(schema.AgentID(name)))
	}
	return exposed, nil
}

// applyCard copies a card override into an AgentExpose.
func applyCard(expose *schema.AgentExpose, card cardConfig) {
	expose.Name = card.Name
	expose.Description = card.Description
	expose.Version = card.Version
	expose.DocumentationURL = card.DocumentationURL
	expose.IconURL = card.IconURL
	if card.Provider != nil {
		expose.Provider = &schema.AgentProvider{
			Organization: card.Provider.Organization,
			URL:          card.Provider.URL,
		}
	}
	expose.Skills = make([]schema.AgentSkill, 0, len(card.Skills))
	for _, skill := range card.Skills {
		expose.Skills = append(expose.Skills, schema.AgentSkill{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        skill.Tags,
			Examples:    skill.Examples,
			InputModes:  skill.InputModes,
			OutputModes: skill.OutputModes,
		})
	}
}

// validateSkills rejects a card skill whose id is not a real capability of the
// agent, so the card cannot advertise something the program cannot do.
func validateSkills(client *agent.Client, agentID string, skills []skillConfig) error {
	if len(skills) == 0 {
		return nil
	}
	ids, err := capabilityIDs(client, agentID)
	if err != nil {
		return err
	}
	for _, skill := range skills {
		if skill.ID == "" {
			return fmt.Errorf("agent %q: card skill requires an id", agentID)
		}
		if !ids[skill.ID] {
			return fmt.Errorf("agent %q: card skill %q is not one of its capabilities", agentID, skill.ID)
		}
	}
	return nil
}

// capabilityIDs returns the ids of an agent's capabilities: its tools,
// delegatable subagents and loaded skills.
func capabilityIDs(client *agent.Client, agentID string) (map[string]bool, error) {
	ids := map[string]bool{}
	tools, err := client.AgentTools(agentID, nil)
	if err != nil {
		return nil, err
	}
	for _, tool := range tools {
		ids[tool.Name] = true
	}
	allowAll := schema.PermissionRuleset{{Action: "*", Resource: "*", Effect: schema.PermissionEffectAllow}}
	for _, subagent := range agent.SubagentTypes(client.Agents(), allowAll, language.English) {
		ids[string(subagent.ID)] = true
	}
	skills, err := client.Skills().List()
	if err != nil {
		return nil, err
	}
	for _, skill := range skills {
		ids[skill.Name] = true
	}
	return ids, nil
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
