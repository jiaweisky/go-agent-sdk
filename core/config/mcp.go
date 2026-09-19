package config

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/schema"
)

// MCPTimeout mirrors ConfigMCP.Timeout.
type MCPTimeout struct {
	Startup *schema.PositiveInt `json:"startup,omitempty"`
	Request *schema.PositiveInt `json:"request,omitempty"`
}

// MCPOAuth mirrors ConfigMCP.OAuth.
type MCPOAuth struct {
	ClientID     *string `json:"client_id,omitempty"`
	ClientSecret *string `json:"client_secret,omitempty"`
	Scope        *string `json:"scope,omitempty"`
	CallbackPort *int    `json:"callback_port,omitempty"`
	RedirectURI  *string `json:"redirect_uri,omitempty"`
}

// MCPServer mirrors ConfigMCP.Server (Local | Remote), discriminated by `type`.
type MCPServer struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Cwd         *string           `json:"cwd,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         *string           `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	OAuth       json.RawMessage   `json:"oauth,omitempty"`
	Disabled    *bool             `json:"disabled,omitempty"`
	Timeout     *MCPTimeout       `json:"timeout,omitempty"`
}

// MCPInfo mirrors ConfigMCP.Info.
type MCPInfo struct {
	Timeout *MCPTimeout          `json:"timeout,omitempty"`
	Servers map[string]MCPServer `json:"servers,omitempty"`
}
