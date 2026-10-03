// Package mcp adapts Model Context Protocol servers to the SDK's core tool
// framework. It covers server discovery plus the client and transport
// lifecycle; OAuth, credential storage and interactive events are narrowed
// away, so callers pass a plain ServerConfig.
package mcp

import (
	"time"
)

// DefaultTimeout is the call timeout used when a server config sets none.
const DefaultTimeout = 30 * time.Second

// Connection statuses reported by Status.
const (
	StatusConnected               = "connected"
	StatusDisabled                = "disabled"
	StatusFailed                  = "failed"
	StatusNeedsAuth               = "needs_auth"
	StatusNeedsClientRegistration = "needs_client_registration"
)

// ServerConfig describes a configured MCP server. Type is "local" or "remote".
//
// Static remote authentication is expressed through Headers (for example
// "Authorization": "Bearer <token>" or "X-API-Key": "<key>"). Interactive OAuth
// is intentionally not supported.
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
	// Status is one of the Status* constants.
	Status string
	Error  string
}
