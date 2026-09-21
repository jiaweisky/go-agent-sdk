// Package mcp ports the pure parts of opencode's MCP integration
// (packages/opencode/src/mcp/catalog.ts and the client/transport lifecycle of
// index.ts). OAuth, credential storage, TUI events, and v1 config are narrowed
// away: callers pass a plain ServerConfig.
package mcp

import (
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// DefaultTimeout mirrors DEFAULT_TIMEOUT.
const DefaultTimeout = 30 * time.Second

// ServerConfig describes a configured MCP server. Type is "local" or "remote".
type ServerConfig struct {
	Type string
	// local
	Command     []string
	Cwd         string
	Environment map[string]string
	// remote
	URL     string
	Headers map[string]string
	// OAuth, when set, authorizes remote requests. Callers supply their own
	// handler (the SDK provides auth.AuthorizationCodeHandler); credential
	// storage and the interactive callback are the caller's responsibility.
	OAuth auth.OAuthHandler
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

// Status mirrors MCP.Status (the status union).
type Status struct {
	// Status is one of: connected, disabled, failed, needs_auth,
	// needs_client_registration.
	Status string
	Error  string
}
