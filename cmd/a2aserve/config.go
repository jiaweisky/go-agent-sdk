package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

// config is the a2aserve configuration. It is assembled from built-in defaults,
// then a YAML file, then CLI flags (flags win). No environment variables are
// read.
type config struct {
	Listen             string                `yaml:"listen"`
	BaseURL            string                `yaml:"baseURL"`
	Root               string                `yaml:"root"`
	WorkingDirectory   string                `yaml:"workingDirectory"`
	ConfigDirs         []string              `yaml:"configDirs"`
	Model              string                `yaml:"model"`
	Protocol           string                `yaml:"protocol"`
	Credentials        map[string]credential `yaml:"credentials"`
	Agents             []string              `yaml:"agents"`
	InboundPermissions []permissionRule      `yaml:"inboundPermissions"`
	KeepAlive          string                `yaml:"keepAlive"`
	Auth               *authConfig           `yaml:"auth"`
}

// credential is a provider's secret. Only API keys are supported.
type credential struct {
	APIKey string `yaml:"apiKey"`
}

// permissionRule is one entry of the inbound permission ruleset.
type permissionRule struct {
	Action   string `yaml:"action"`
	Resource string `yaml:"resource"`
	Effect   string `yaml:"effect"`
}

// authConfig enables bearer authentication on the RPC endpoints.
type authConfig struct {
	Bearer string `yaml:"bearer"`
}

// defaultConfig is the baseline every run starts from.
func defaultConfig() config {
	return config{
		Listen:    "127.0.0.1:9099",
		Agents:    []string{"build"},
		KeepAlive: "15s",
	}
}

// loadConfig decodes a YAML file over cfg, so keys the file omits keep their
// prior (default) values. Unknown keys are rejected.
func loadConfig(path string, cfg *config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

// validate reports configuration that cannot produce a running server.
func (c config) validate() error {
	switch {
	case c.Root == "":
		return errors.New("root is required (the SDK state directory)")
	case c.Model == "":
		return errors.New("model is required")
	case c.Protocol == "":
		return errors.New("protocol is required")
	case len(c.Agents) == 0:
		return errors.New("at least one agent is required")
	case len(c.Credentials) == 0:
		return errors.New("credentials are required")
	case c.Auth != nil && c.Auth.Bearer == "":
		return errors.New("auth.bearer is required when auth is set")
	}
	if _, err := c.keepAliveDuration(); err != nil {
		return err
	}
	return nil
}

// keepAliveDuration parses KeepAlive, treating empty as disabled.
func (c config) keepAliveDuration() (time.Duration, error) {
	if c.KeepAlive == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(c.KeepAlive)
	if err != nil {
		return 0, fmt.Errorf("keepAlive: %w", err)
	}
	if duration < 0 {
		return 0, errors.New("keepAlive must not be negative")
	}
	return duration, nil
}

// permissionRuleset converts the configured rules; nil means "use the SDK
// default" (see run).
func (c config) permissionRuleset() []schema.PermissionRule {
	if len(c.InboundPermissions) == 0 {
		return nil
	}
	rules := make([]schema.PermissionRule, 0, len(c.InboundPermissions))
	for _, rule := range c.InboundPermissions {
		rules = append(rules, schema.PermissionRule{
			Action:   rule.Action,
			Resource: rule.Resource,
			Effect:   schema.PermissionEffect(rule.Effect),
		})
	}
	return rules
}
