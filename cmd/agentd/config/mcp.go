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

// MCPEntry is a validated MCP server and its parsed timeout.
type MCPEntry struct {
	Server  MCPServer
	Timeout time.Duration
}

// Validate checks one MCP server entry.
func (s MCPServer) Validate() error {
	_, err := s.validateAndParse()
	return err
}

// validateAndParse checks one server and returns its parsed timeout.
func (s MCPServer) validateAndParse() (time.Duration, error) {
	switch {
	case s.Name == "":
		return 0, errors.New("mcp: each server requires a name")
	case s.Type == "":
		return 0, fmt.Errorf("mcp %s: type is required (local or remote)", s.Name)
	case !s.Type.Valid():
		return 0, fmt.Errorf("mcp %s: type must be local or remote, got %q", s.Name, s.Type)
	case s.Type == MCPLocal && len(s.Command) == 0:
		return 0, fmt.Errorf("mcp %s: local requires command", s.Name)
	case s.Type == MCPRemote && s.URL == "":
		return 0, fmt.Errorf("mcp %s: remote requires url", s.Name)
	}

	timeout, err := ParseOptionalDuration(s.Timeout)
	if err != nil {
		return 0, fmt.Errorf("mcp %s: timeout: %w", s.Name, err)
	}

	return timeout, nil
}

// ValidateMCP checks that every MCP server entry is well formed.
func ValidateMCP(servers []MCPServer) error {
	_, err := ValidateMCPConfigs(servers)
	return err
}

// ValidateMCPConfigs validates the server entries and returns them with parsed
// timeouts (zero when unset), so a converting caller need not parse them again.
func ValidateMCPConfigs(servers []MCPServer) ([]MCPEntry, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	entries := make([]MCPEntry, len(servers))
	seen := make(map[string]bool, len(servers))

	for i, server := range servers {
		timeout, err := server.validateAndParse()
		if err != nil {
			return nil, err
		}

		if seen[server.Name] {
			return nil, fmt.Errorf("mcp %s: duplicate name", server.Name)
		}

		seen[server.Name] = true
		entries[i] = MCPEntry{Server: server, Timeout: timeout}
	}

	return entries, nil
}

// MCPTimeouts returns each server's parsed timeout.
//
// Deprecated: use ValidateMCPConfigs, which also returns the validated server
// entries.
func MCPTimeouts(servers []MCPServer) ([]time.Duration, error) {
	entries, err := ValidateMCPConfigs(servers)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	timeouts := make([]time.Duration, len(entries))
	for i, entry := range entries {
		timeouts[i] = entry.Timeout
	}

	return timeouts, nil
}
