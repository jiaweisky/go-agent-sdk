package config

import (
	"errors"
	"fmt"
	"time"
)

// MCPType selects how agentd starts or reaches an MCP server.
type MCPType string

const (
	// MCPLocal runs a local stdio MCP server.
	MCPLocal MCPType = "local"
	// MCPRemote connects to a remote StreamableHTTP MCP server.
	MCPRemote MCPType = "remote"
)

// Valid reports whether t is a supported MCP transport type.
func (t MCPType) Valid() bool {
	return t == MCPLocal || t == MCPRemote
}

// MCPServer configures one MCP server (local stdio or remote StreamableHTTP).
type MCPServer struct {
	Name        string            `yaml:"name"`
	Type        MCPType           `yaml:"type"`
	Command     []string          `yaml:"command"`
	Cwd         string            `yaml:"cwd"`
	Environment map[string]string `yaml:"environment"`
	URL         string            `yaml:"url"`
	Headers     map[string]string `yaml:"headers"`
	Enabled     *bool             `yaml:"enabled"`
	Timeout     string            `yaml:"timeout"`
}

// parseOptionalDuration parses a duration string; an empty string means zero.
func parseOptionalDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if duration < 0 {
		return 0, errors.New("must not be negative")
	}
	return duration, nil
}

// ValidateMCP checks that every MCP server entry is well formed.
func ValidateMCP(servers []MCPServer) error {
	for _, server := range servers {
		switch {
		case server.Name == "":
			return errors.New("mcp: each server requires a name")
		case server.Type == "":
			return fmt.Errorf("mcp %s: type is required (local or remote)", server.Name)
		case !server.Type.Valid():
			return fmt.Errorf("mcp %s: type must be local or remote, got %q", server.Name, server.Type)
		case server.Type == MCPLocal && len(server.Command) == 0:
			return fmt.Errorf("mcp %s: local requires command", server.Name)
		case server.Type == MCPRemote && server.URL == "":
			return fmt.Errorf("mcp %s: remote requires url", server.Name)
		}
		if _, err := parseOptionalDuration(server.Timeout); err != nil {
			return fmt.Errorf("mcp %s: timeout: %w", server.Name, err)
		}
	}
	return nil
}
