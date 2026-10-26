package client

import (
	"context"
	"fmt"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/modelinfo"
)

// Params is the set of options the agentd assembly uses when building an agent
// client. The static server builder and the dynamic project builder map their
// configuration onto this type so the assembly lives in one place.
type Params struct {
	Root             string
	WorkingDirectory string
	ConfigDirs       []string

	// Model is the default model id reported to the client.
	Model string
	// ModelRegistry resolves per-model metadata and credentials.
	ModelRegistry config.ModelRegistry

	MediaUploader  func(schema.ProviderID) schema.MediaUploader
	Media          *core.MediaPolicy
	MediaTranscode func(core.MediaTranscodeRequest) (core.MediaTranscodeResult, bool)
	// MediaFileHeaders carries provider file-reference headers per provider.
	MediaFileHeaders map[schema.ProviderID]map[string]string
	Store            schema.MediaStore

	MCP        []agent.MCPConfig
	References map[string]core.ReferenceSource

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

// New assembles an agent client from Params with a background startup context.
func New(p Params) (*agent.Client, error) {
	return NewContext(context.Background(), p)
}

// NewContext is New with a context that bounds startup work such as connecting
// MCP servers.
func NewContext(ctx context.Context, p Params) (*agent.Client, error) {
	if err := config.ValidateA2APeers(p.Peers); err != nil {
		return nil, err
	}
	if len(p.ModelRegistry.IDs()) == 0 {
		return nil, fmt.Errorf("model registry has no models")
	}

	opts := &agent.ClientOptions{
		Root:             p.Root,
		WorkingDirectory: p.WorkingDirectory,
		ConfigDirs:       p.ConfigDirs,
		Model:            p.Model,
		ModelInfo:        func(id string) (schema.ModelInfo, error) { return modelinfo.Resolve(p.ModelRegistry, id) },
		Credentials:      p.ModelRegistry.Credentials,
		MediaUploader:    p.MediaUploader,
		MediaPolicy:      p.Media,
		MediaStore:       p.Store,
		MediaTranscode:   p.MediaTranscode,
		MediaFileHeaders: p.MediaFileHeaders,
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

	client, err := agent.NewClientContext(ctx, opts)
	if err != nil {
		return nil, err
	}

	if err := registerA2APeers(client, p.Peers); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// MCPConfigs validates configured MCP servers and converts them into SDK client
// configs, with parsed timeouts.
func MCPConfigs(servers []config.MCPServer) ([]agent.MCPConfig, error) {
	entries, err := config.ValidateMCPConfigs(servers)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	result := make([]agent.MCPConfig, 0, len(entries))
	for _, entry := range entries {
		server := entry.Server
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
				Timeout:     entry.Timeout,
			},
		})
	}
	return result, nil
}

// ModelInfos resolves every registry model's metadata for discovery.
func ModelInfos(registry config.ModelRegistry) ([]schema.ModelInfo, error) {
	return modelinfo.List(registry)
}
