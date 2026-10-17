package config

import (
	"errors"
	"fmt"
)

// ProjectRequest is the body of the control API's POST /admin/projects: one
// isolated project instance to create. Root and WorkingDirectory must be
// absolute host paths; Isolation is optional (default: none). It is the shared
// contract between the agentd admin API and the supervisor command.
type ProjectRequest struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Root             string   `json:"root"`
	WorkingDirectory string   `json:"workingDirectory"`
	ConfigDirs       []string `json:"configDirs,omitempty"`
	Models           []string `json:"models,omitempty"`
	DefaultModel     string   `json:"defaultModel,omitempty"`
	Agents           []string `json:"agents"`
	// Peers are remote A2A agents the instance reaches as a client (same shape
	// as a server's).
	Peers     []Peer           `json:"peers,omitempty"`
	Isolation *IsolationConfig `json:"isolation,omitempty"`
	// InboundPermissions overrides the instance's tool policy (same shape as a
	// server's). Empty means the SDK default (read-only).
	InboundPermissions []PermissionRule `json:"inboundPermissions,omitempty"`
	// Sandbox selects the command-execution seam for the instance's tools (same
	// shape as a server's). Empty means the SDK pass-through.
	Sandbox *SandboxConfig `json:"sandbox,omitempty"`
	// Readonly marks a read-only lease. Read-only leases share the source
	// directory and may coexist (several readers); a writable lease is exclusive.
	// It is therefore mutually exclusive with Isolation.
	Readonly bool `json:"readonly,omitempty"`
	// Auth, when set, guards the instance's transport endpoints.
	Auth *AuthConfig `json:"auth,omitempty"`
}

// Validate checks a project request before its instance is created.
func (r ProjectRequest) Validate() error {
	switch {
	case r.Name == "":
		return errors.New("name is required")
	case r.Kind != "http" && r.Kind != "a2a":
		return errors.New("kind must be http or a2a")
	case r.Root == "":
		return errors.New("root is required")
	case len(r.Agents) == 0:
		return errors.New("at least one agent is required")
	}
	if r.Readonly && r.Isolation != nil {
		return errors.New("readonly shares the source directory; isolation is not allowed")
	}
	if err := validateAuthConfig(r.Kind, r.Auth); err != nil {
		return err
	}
	if err := ValidateSandbox(r.Sandbox); err != nil {
		return fmt.Errorf("project %q: %w", r.Name, err)
	}
	if err := ValidatePeers(r.Peers); err != nil {
		return fmt.Errorf("project %q: %w", r.Name, err)
	}
	return ValidateIsolation(r.Name, r.WorkingDirectory, r.Isolation)
}

// ToServer maps a project request to the config.Server an instance serves. root
// is the resolved (absolute) state root.
func (r ProjectRequest) ToServer(root string) Server {
	return Server{
		Name:               r.Name,
		Kind:               r.Kind,
		Root:               root,
		WorkingDirectory:   r.WorkingDirectory,
		ConfigDirs:         r.ConfigDirs,
		Models:             r.Models,
		DefaultModel:       r.DefaultModel,
		Agents:             r.Agents,
		Peers:              r.Peers,
		Isolation:          r.Isolation,
		InboundPermissions: r.InboundPermissions,
		Sandbox:            r.Sandbox,
		Auth:               r.Auth,
	}
}
