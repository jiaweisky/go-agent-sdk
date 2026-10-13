package main

import (
	"errors"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

// supervisorConfig configures the reuse-oriented project supervisor: a running
// agentd admin (control) API plus the template every acquired project is cloned
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
	return nil
}

// endpoint is a running agentd's control (admin) API.
type endpoint struct {
	BaseURL string `yaml:"baseURL"`
	Token   string `yaml:"token"`
}

// projectTemplate describes the isolated instance created per project. rootBase
// plus the project id forms the SDK state root; the agentd admin API applies the
// isolation (worktree/overlay/copy) and prepares the view.
type projectTemplate struct {
	Kind             string                  `yaml:"kind"` // http | a2a
	RootBase         string                  `yaml:"rootBase"`
	WorkingDirectory string                  `yaml:"workingDirectory"`
	ConfigDirs       []string                `yaml:"configDirs"`
	Models           []string                `yaml:"models"`
	DefaultModel     string                  `yaml:"defaultModel"`
	Agents           []string                `yaml:"agents"`
	Isolation        *config.IsolationConfig `yaml:"isolation"`
	// Sandbox selects the command-execution seam for the instance's tools (same
	// shape as a server's); empty means the SDK pass-through.
	Sandbox *config.SandboxConfig `yaml:"sandbox"`
	Auth    *config.AuthConfig    `yaml:"auth"`
}
