package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/server"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// run builds the SDK client and A2A server from cfg and serves until ctx is
// canceled, then shuts down gracefully and closes the client.
func run(ctx context.Context, cfg config) error {
	keepAlive, err := cfg.keepAliveDuration()
	if err != nil {
		return err
	}

	catalog, err := cfg.modelCatalog()
	if err != nil {
		return err
	}
	model := cfg.Model
	if model == "" {
		model = catalog.DefaultID()
	}
	client, err := agent.NewClient(&agent.ClientOptions{
		Root:             cfg.Root,
		WorkingDirectory: cfg.WorkingDirectory,
		ConfigDirs:       cfg.ConfigDirs,
		Model:            model,
		Protocol:         schema.ProtocolID(cfg.Protocol),
		ModelInfo:        catalog.ModelInfo,
		Credentials:      catalog.Credentials,
		MediaUploader:    cfg.mediaUploader(),
		DropErroredTurns: cfg.DropErroredTurns,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	models, err := cfg.modelInfos()
	if err != nil {
		return err
	}
	agents, err := exposeAgents(client, cfg.Agents, cfg.Cards, modelModes(models), cfg.Model)
	if err != nil {
		return err
	}

	factoryOptions := []agent.A2AFactoryOption{}
	if permissions := serveconfig.PermissionRuleset(cfg.InboundPermissions); len(permissions) > 0 {
		factoryOptions = append(factoryOptions, agent.WithInboundPermissions(permissions))
	}
	if cfg.InboundMedia != "" {
		factoryOptions = append(factoryOptions, agent.WithInboundMedia(cfg.InboundMedia))
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
// cards optionally overrides an agent's card; configured skills are validated
// as well-formed (id, name and description).
func exposeAgents(client *agent.Client, names []string, cards map[string]cardConfig, modes map[string][]string, defaultModel string) ([]schema.AgentInfo, error) {
	exposed := make([]schema.AgentInfo, 0, len(names))
	for _, name := range names {
		info := client.Agents().Get(schema.AgentID(name))
		if info == nil {
			return nil, fmt.Errorf("unknown agent %q", name)
		}
		expose := &schema.AgentExpose{}
		if override, ok := cards[name]; ok {
			if err := validateSkills(name, override.Skills); err != nil {
				return nil, err
			}
			applyCard(expose, override)
		}
		// Advertise the agent's model media modes on the card when the
		// card config did not set them.
		if len(expose.InputModes) == 0 {
			modelID := defaultModel
			if info.Model != nil {
				modelID = string(info.Model.ID)
			}
			if agentModes := modes[modelID]; len(agentModes) > 0 {
				expose.InputModes = agentModes
			}
		}
		if err := client.RegisterAgent(name, agent.AgentConfig{Expose: expose}); err != nil {
			return nil, fmt.Errorf("expose agent %q: %w", name, err)
		}
		exposed = append(exposed, *client.Agents().Get(schema.AgentID(name)))
	}
	return exposed, nil
}

// modelModes maps each catalog model id to the A2A input MIME modes its
// modalities imply.
func modelModes(models []schema.ModelInfo) map[string][]string {
	out := make(map[string][]string, len(models))
	for _, model := range models {
		out[string(model.ID)] = modalityMIMEs(model.Capabilities.Input)
	}
	return out
}

// modalityMIMEs maps declared modalities to A2A input MIME modes.
func modalityMIMEs(modalities []string) []string {
	if len(modalities) == 0 {
		return nil
	}
	modes := []string{"text/plain", "application/json"}
	for _, modality := range modalities {
		switch modality {
		case "image":
			modes = append(modes, "image/png", "image/jpeg", "image/gif", "image/webp")
		case "audio":
			modes = append(modes, "audio/wav", "audio/mp3")
		case "video":
			modes = append(modes, "video/mp4", "video/webm", "video/quicktime")
		case "pdf":
			modes = append(modes, "application/pdf")
		}
	}
	return modes
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

// validateSkills requires each configured card skill to carry id, name and
// description — the fields A2A marks required. Skills are operator-authored
// descriptive labels advertised on the card; they are not tied to the agent's
// internal tools/subagents, and an agent with none advertises an empty list.
func validateSkills(agentID string, skills []skillConfig) error {
	for i, skill := range skills {
		if skill.ID == "" || skill.Name == "" || skill.Description == "" {
			return fmt.Errorf("agent %q: skills[%d] requires id, name, and description", agentID, i)
		}
	}
	return nil
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
