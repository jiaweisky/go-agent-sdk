package config

import "github.com/user/go-agent-sdk/schema"

// V1MCPOAuth mirrors ConfigMCPV1.OAuth.
type V1MCPOAuth struct {
	ClientID     *string `json:"clientId,omitempty"`
	ClientSecret *string `json:"clientSecret,omitempty"`
	Scope        *string `json:"scope,omitempty"`
	CallbackPort *int    `json:"callbackPort,omitempty"`
	RedirectURI  *string `json:"redirectUri,omitempty"`
}

// V1MCPServer mirrors ConfigMCPV1.Info (Local | Remote). `type` is optional
// because the v1 config also allows a bare `{ enabled }` entry.
type V1MCPServer struct {
	Type        *string             `json:"type,omitempty"`
	Command     []string            `json:"command,omitempty"`
	Cwd         *string             `json:"cwd,omitempty"`
	Environment map[string]string   `json:"environment,omitempty"`
	Enabled     *bool               `json:"enabled,omitempty"`
	URL         *string             `json:"url,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	OAuth       *V1MCPOAuth         `json:"oauth,omitempty"`
	Timeout     *schema.PositiveInt `json:"timeout,omitempty"`
}
