// Package mcp ports the pure parts of opencode's MCP integration
// (packages/opencode/src/mcp/catalog.ts and the client/transport lifecycle of
// index.ts). OAuth, credential storage, TUI events, and v1 config are narrowed
// away: callers pass a plain ServerConfig.
package mcp

import (
	"time"
)

// DefaultTimeout is the call timeout used when a server config sets none.
const DefaultTimeout = 30 * time.Second

// ServerConfig describes a configured MCP server. Type is "local" or "remote".
//
// Static remote authentication is expressed through Headers, matching opencode's
// McpRemoteConfig (for example "Authorization": "Bearer <token>" or
// "X-API-Key": "<key>"). Interactive OAuth is intentionally not supported.
type ServerConfig struct {
	Type string
	// local
	Command     []string
	Cwd         string
	Environment map[string]string
	// remote
	URL     string
	Headers map[string]string
	// shared
	Enabled *bool
	Timeout time.Duration
}

func (s ServerConfig) isEnabled() bool { return s.Enabled == nil || *s.Enabled }

func (s ServerConfig) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return DefaultTimeout
}

// Status is a server's connection status.
type Status struct {
	// Status is one of: connected, disabled, failed, needs_auth,
	// needs_client_registration.
	Status string
	Error  string
}
