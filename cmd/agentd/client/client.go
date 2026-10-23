package client

import (
	"fmt"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/modelinfo"
)

// Params is the set of options the serve commands share when building an agent
// client. Each command maps its own configuration onto this type so the
// assembly lives in one place.
type Params struct {
	Root             string
	WorkingDirectory string
	ConfigDirs       []string
	// Protocol is the wire protocol id forwarded to the client, alongside the
	// resolved Catalog ModelInfo.
	Protocol string
	// Model is the default model id reported to the client.
	Model string
	// Catalog resolves per-model metadata and credentials.
	Catalog        config.Catalog
	MediaUploader  func(schema.ProviderID) schema.MediaUploader
	Media          *core.MediaPolicy
	MediaTranscode func(core.MediaTranscodeRequest) (core.MediaTranscodeResult, bool)
	Store          schema.MediaStore
	MCP            []agent.MCPConfig
	References     map[string]core.ReferenceSource
	// Peers are remote A2A agents registered as agents after the client starts.
	Peers            []config.A2APeer
	SubagentDepth    int
	TitleAuto        bool
	DropErroredTurns bool
	// Process is the command-execution seam; nil keeps the SDK default.
	Process core.AppProcess
	// FileReadGuard / FileWriteGuard enforce the sandbox policy on the in-process
	// file tools, which the process seam cannot see.
	FileReadGuard  core.FileReadGuard
	FileWriteGuard core.FileWriteGuard
	// SearchExcludes are ripgrep glob patterns the glob/grep tools omit (e.g. an
	// isolation view under the project).
	SearchExcludes []string
}

// New assembles an agent client from Params.
func New(p Params) (*agent.Client, error) {
	if err := config.ValidateA2APeers(p.Peers); err != nil {
		return nil, err
	}
	opts := &agent.ClientOptions{
		Root:             p.Root,
		WorkingDirectory: p.WorkingDirectory,
		ConfigDirs:       p.ConfigDirs,
		Model:            p.Model,
		Protocol:         schema.ProtocolID(p.Protocol),
		ModelInfo:        func(id string) (schema.ModelInfo, error) { return modelinfo.Resolve(p.Catalog, id) },
		Credentials:      p.Catalog.Credentials,
		MediaUploader:    p.MediaUploader,
		MediaPolicy:      p.Media,
		MediaStore:       p.Store,
		MediaTranscode:   p.MediaTranscode,
		MCP:              p.MCP,
		References:       p.References,
		DropErroredTurns: p.DropErroredTurns,
		Process:          p.Process,
		FileReadGuard:    p.FileReadGuard,
		FileWriteGuard:   p.FileWriteGuard,
		SearchExcludes:   p.SearchExcludes,
	}
	if p.SubagentDepth > 0 {
		opts.SubagentDepth = p.SubagentDepth
	}
	if p.TitleAuto {
		auto := true
		opts.Title = &agent.TitleConfig{Auto: &auto}
	}
	client, err := agent.NewClient(opts)
	if err != nil {
		return nil, err
	}
	if err := registerA2APeers(client, p.Peers); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// MCPConfigs converts configured MCP servers into SDK client configs.
func MCPConfigs(servers []config.MCPServer) ([]agent.MCPConfig, error) {
	timeouts, err := config.MCPTimeouts(servers)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, nil
	}
	result := make([]agent.MCPConfig, 0, len(servers))
	for i, server := range servers {
		result = append(result, agent.MCPConfig{
			Name: server.Name,
			Server: mcp.ServerConfig{
				Type:        string(server.Type),
				Command:     server.Command,
				Cwd:         server.Cwd,
				Environment: server.Environment,
				URL:         server.URL,
				Headers:     server.Headers,
				Enabled:     server.Enabled,
				Timeout:     timeouts[i],
			},
		})
	}
	return result, nil
}

// registerA2APeers registers each configured A2A peer as an agent with an A2A
// backend. A peer whose name collides with an existing agent (a built-in or a
// ConfigDirs agent) is rejected rather than silently rewriting that agent's
// backend.
func registerA2APeers(client *agent.Client, peers []config.A2APeer) error {
	for _, peer := range peers {
		if client.Agents().Get(schema.AgentID(peer.Name)) != nil {
			return fmt.Errorf("peer %s: an agent with that name already exists", peer.Name)
		}
		if err := client.RegisterAgent(peer.Name, a2aPeerAgentConfig(peer)); err != nil {
			return fmt.Errorf("peer %s: %w", peer.Name, err)
		}
	}
	return nil
}

// a2aPeerAgentConfig maps a configured peer to the agent config that registers
// it with an A2A backend.
func a2aPeerAgentConfig(peer config.A2APeer) agent.AgentConfig {
	mode := peer.ModeOrDefault()
	return agent.AgentConfig{
		Description: peer.Description,
		Mode:        &mode,
		Backend: &schema.AgentBackend{
			Type: schema.AgentBackendA2A,
			A2A: &schema.AgentA2ABackend{
				Endpoint: peer.Endpoint,
				CardURL:  peer.CardURL,
				Headers:  peer.Headers,
			},
		},
	}
}

// ModelInfos resolves every catalog model's metadata for discovery.
func ModelInfos(catalog config.Catalog) ([]schema.ModelInfo, error) {
	return modelinfo.List(catalog)
}
