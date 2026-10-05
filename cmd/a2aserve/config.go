package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
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
	Credentials        credentialsConfig     `yaml:"credentials"`
	Agents             []string              `yaml:"agents"`
	Cards              map[string]cardConfig `yaml:"cards"`
	ContextLimit       int                   `yaml:"contextLimit"`
	OutputLimit        int                   `yaml:"outputLimit"`
	InboundPermissions []permissionRule      `yaml:"inboundPermissions"`
	KeepAlive          string                `yaml:"keepAlive"`
	Auth               *authConfig           `yaml:"auth"`
	TLS                *tlsConfig            `yaml:"tls"`
	// AllowInsecure permits binding a non-loopback address without auth. It is
	// an explicit opt-out of the safe default.
	AllowInsecure bool `yaml:"allowInsecure"`
}

// cardConfig overrides an exposed agent's card name and description.
type cardConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// tlsConfig enables HTTPS when both files are set.
type tlsConfig struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

// credentialsConfig holds the credential for the configured protocol: an API
// key for bearer/header protocols, or AWS SigV4 for Bedrock.
type credentialsConfig struct {
	APIKey string          `yaml:"apiKey"`
	AWS    *awsCredentials `yaml:"aws"`
}

// awsCredentials is the SigV4 credential for Amazon Bedrock.
type awsCredentials struct {
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"accessKeyId"`
	SecretAccessKey string `yaml:"secretAccessKey"`
	SessionToken    string `yaml:"sessionToken"`
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

// isLoopback reports whether Listen binds only the loopback interface.
func (c config) isLoopback() bool {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return false
	}
	if host == "" {
		return false // empty host binds all interfaces
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	case c.Credentials.APIKey == "" && c.Credentials.AWS == nil:
		return errors.New("credentials must set apiKey or aws")
	case c.Auth != nil && c.Auth.Bearer == "":
		return errors.New("auth.bearer is required when auth is set")
	case !c.isLoopback() && !c.AllowInsecure && c.Auth == nil:
		return errors.New(
			"refusing to bind a non-loopback address without auth; set auth.bearer, bind loopback, or allowInsecure",
		)
	}
	if aws := c.Credentials.AWS; aws != nil {
		if aws.Region == "" || aws.AccessKeyID == "" || aws.SecretAccessKey == "" {
			return errors.New("aws credentials require region, accessKeyId and secretAccessKey")
		}
	}
	if c.ContextLimit < 0 || c.OutputLimit < 0 {
		return errors.New("contextLimit and outputLimit must not be negative")
	}
	if tls := c.TLS; tls != nil && (tls.CertFile == "" || tls.KeyFile == "") {
		return errors.New("tls requires both certFile and keyFile")
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
