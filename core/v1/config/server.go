package config

import "github.com/user/go-agent-sdk/schema"

// V1ServerInfo mirrors ConfigServerV1.Server.
type V1ServerInfo struct {
	Port       *schema.PositiveInt `json:"port,omitempty"`
	Hostname   *string             `json:"hostname,omitempty"`
	MDNS       *bool               `json:"mdns,omitempty"`
	MDNSDomain *string             `json:"mdnsDomain,omitempty"`
	CORS       []string            `json:"cors,omitempty"`
}
