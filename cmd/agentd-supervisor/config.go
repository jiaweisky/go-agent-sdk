package main

import (
	"errors"
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// supervisorConfig configures the reuse-oriented project supervisor: a running
// agentd control API plus the template every acquired project is cloned
// from. It is loaded with the agentd config loader, so `${VAR}` references and
// strict, unknown-key-rejecting parsing apply too.
type supervisorConfig struct {
	Agentd  endpoint        `yaml:"agentd"`
	Project projectTemplate `yaml:"project"`
}

// Validate checks the fields the supervisor needs to serve requests.
func (c supervisorConfig) Validate() error {
	if c.Agentd.BaseURL == "" {
		return errors.New("agentd.baseURL is required")
	}
	if len(c.Project.Agents) == 0 {
		return errors.New("project.agents is required")
	}
	if c.Project.Kind != "" && !c.Project.Kind.Valid() {
		return fmt.Errorf("project.kind must be %s or %s, got %q", config.KindHTTP, config.KindA2A, c.Project.Kind)
	}
	if err := config.ValidateA2APeers(c.Project.Peers); err != nil {
		return fmt.Errorf("project: %w", err)
	}
	if err := c.Project.Sandbox.Validate(); err != nil {
		return fmt.Errorf("project: %w", err)
	}
	if err := c.Project.Auth.Validate(); err != nil {
		return fmt.Errorf("project: %w", err)
	}
	return nil
}

// endpoint is a running agentd's control API.
type endpoint struct {
	BaseURL string `yaml:"baseURL"`
	Token   string `yaml:"token"`
}

// projectTemplate describes the isolated instance created per project. rootBase
// plus the project id forms the SDK state root; the agentd control API applies the
// isolation (worktree/overlay/copy) and prepares the view.
type projectTemplate struct {
	Kind             config.Kind             `yaml:"kind"` // config.KindHTTP | config.KindA2A
	RootBase         string                  `yaml:"rootBase"`
	WorkingDirectory string                  `yaml:"workingDirectory"`
	ConfigDirs       []string                `yaml:"configDirs"`
	Models           []string                `yaml:"models"`
	DefaultModel     string                  `yaml:"defaultModel"`
	Agents           []string                `yaml:"agents"`
	Peers            []config.A2APeer        `yaml:"peers"`
	Isolation        *config.IsolationConfig `yaml:"isolation"`
	// Sandbox selects the command-execution seam for the instance's tools (same
	// shape as a server's); empty means the SDK pass-through.
	Sandbox *config.SandboxConfig `yaml:"sandbox"`
	Auth    *config.AuthConfig    `yaml:"auth"`
}
