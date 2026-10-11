package config

import (
	"errors"
	"fmt"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// MCPServer configures one MCP server (local stdio or remote StreamableHTTP).
type MCPServer struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Command     []string          `yaml:"command"`
	Cwd         string            `yaml:"cwd"`
	Environment map[string]string `yaml:"environment"`
	URL         string            `yaml:"url"`
	Headers     map[string]string `yaml:"headers"`
	Enabled     *bool             `yaml:"enabled"`
	Timeout     string            `yaml:"timeout"`
}

// Reference configures one named reference source (local path or git repository)
// injected into the system context.
type Reference struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"` // local | git
	Path        string `yaml:"path"`
	Repository  string `yaml:"repository"`
	Branch      string `yaml:"branch"`
	Description string `yaml:"description"`
	Hidden      *bool  `yaml:"hidden"`
}

// parseOptionalDuration parses a duration string; an empty string means zero.
func parseOptionalDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

// MCPServers converts configured MCP servers into SDK client configs.
func MCPServers(servers []MCPServer) ([]agent.MCPConfig, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	result := make([]agent.MCPConfig, 0, len(servers))
	for _, server := range servers {
		timeout, err := parseOptionalDuration(server.Timeout)
		if err != nil {
			return nil, fmt.Errorf("mcp %s: timeout: %w", server.Name, err)
		}
		result = append(result, agent.MCPConfig{
			Name: server.Name,
			Server: mcp.ServerConfig{
				Type:        server.Type,
				Command:     server.Command,
				Cwd:         server.Cwd,
				Environment: server.Environment,
				URL:         server.URL,
				Headers:     server.Headers,
				Enabled:     server.Enabled,
				Timeout:     timeout,
			},
		})
	}
	return result, nil
}

// ValidateMCP checks that every MCP server entry is well formed.
func ValidateMCP(servers []MCPServer) error {
	for _, server := range servers {
		switch {
		case server.Name == "":
			return errors.New("mcp: each server requires a name")
		case server.Type == "":
			return fmt.Errorf("mcp %s: type is required (local or remote)", server.Name)
		case server.Type == "local" && len(server.Command) == 0:
			return fmt.Errorf("mcp %s: local requires command", server.Name)
		case server.Type == "remote" && server.URL == "":
			return fmt.Errorf("mcp %s: remote requires url", server.Name)
		}
		if _, err := parseOptionalDuration(server.Timeout); err != nil {
			return fmt.Errorf("mcp %s: timeout: %w", server.Name, err)
		}
	}
	return nil
}

// ReferenceSources converts configured references into SDK reference sources.
func ReferenceSources(refs []Reference) (map[string]core.ReferenceSource, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make(map[string]core.ReferenceSource, len(refs))
	for _, ref := range refs {
		if ref.Name == "" {
			return nil, errors.New("references: each requires a name")
		}
		switch ref.Type {
		case "local":
			if ref.Path == "" {
				return nil, fmt.Errorf("reference %s: local requires path", ref.Name)
			}
			out[ref.Name] = schema.ReferenceLocalSource{
				Type:        "local",
				Path:        schema.AbsolutePath(ref.Path),
				Description: optionalString(ref.Description),
				Hidden:      ref.Hidden,
			}
		case "git":
			if ref.Repository == "" {
				return nil, fmt.Errorf("reference %s: git requires repository", ref.Name)
			}
			source := schema.ReferenceGitSource{
				Type:        "git",
				Repository:  ref.Repository,
				Description: optionalString(ref.Description),
				Hidden:      ref.Hidden,
			}
			if ref.Branch != "" {
				branch := ref.Branch
				source.Branch = &branch
			}
			out[ref.Name] = source
		default:
			return nil, fmt.Errorf("reference %s: unknown type %q (local or git)", ref.Name, ref.Type)
		}
	}
	return out, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ClientParams is the set of options the serve commands share when building an
// agent client. Each command maps its own configuration onto this type so the
// assembly lives in one place.
type ClientParams struct {
	Root             string
	WorkingDirectory string
	ConfigDirs       []string
	// Protocol is the wire protocol id forwarded to the client, alongside the
	// resolved Catalog ModelInfo.
	Protocol string
	// Model is the default model id reported to the client.
	Model string
	// Catalog resolves per-model metadata and credentials.
	Catalog          Catalog
	MediaUploader    func(schema.ProviderID) schema.MediaUploader
	MCP              []agent.MCPConfig
	References       map[string]core.ReferenceSource
	SubagentDepth    int
	TitleAuto        bool
	DropErroredTurns bool
}

// BuildClient assembles an agent client from ClientParams.
func BuildClient(p ClientParams) (*agent.Client, error) {
	opts := &agent.ClientOptions{
		Root:             p.Root,
		WorkingDirectory: p.WorkingDirectory,
		ConfigDirs:       p.ConfigDirs,
		Model:            p.Model,
		Protocol:         schema.ProtocolID(p.Protocol),
		ModelInfo:        p.Catalog.ModelInfo,
		Credentials:      p.Catalog.Credentials,
		MediaUploader:    p.MediaUploader,
		MCP:              p.MCP,
		References:       p.References,
		DropErroredTurns: p.DropErroredTurns,
	}
	if p.SubagentDepth > 0 {
		opts.SubagentDepth = p.SubagentDepth
	}
	if p.TitleAuto {
		auto := true
		opts.Title = &agent.TitleConfig{Auto: &auto}
	}
	return agent.NewClient(opts)
}
