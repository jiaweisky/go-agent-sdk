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

// config is the agentserve configuration, assembled from defaults, then a YAML
// file, then CLI flags. No environment variables are read.
type config struct {
	Listen           string            `yaml:"listen"`
	Root             string            `yaml:"root"`
	WorkingDirectory string            `yaml:"workingDirectory"`
	ConfigDirs       []string          `yaml:"configDirs"`
	Model            string            `yaml:"model"`
	Protocol         string            `yaml:"protocol"`
	ModelBaseURL     string            `yaml:"modelBaseURL"`
	Credentials      credentialsConfig `yaml:"credentials"`
	ContextLimit     int               `yaml:"contextLimit"`
	OutputLimit      int               `yaml:"outputLimit"`
	SubagentDepth    int               `yaml:"subagentDepth"`
	TitleAuto        bool              `yaml:"titleAuto"`
	Agents           []string          `yaml:"agents"`

	Auth                *authConfig      `yaml:"auth"`
	InboundPermissions  []permissionRule `yaml:"inboundPermissions"`
	InteractionTimeout  string           `yaml:"interactionTimeout"`
	InteractionFallback string           `yaml:"interactionFallback"`
	SubscriberCapacity  int              `yaml:"subscriberCapacity"`
	KeepAlive           string           `yaml:"keepAlive"`
	PublicRoutes        []string         `yaml:"publicRoutes"`
	CORS                []string         `yaml:"cors"`

	TLS           *tlsConfig `yaml:"tls"`
	AllowInsecure bool       `yaml:"allowInsecure"`
}

type authConfig struct {
	Bearer   string `yaml:"bearer"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type credentialsConfig struct {
	APIKey string          `yaml:"apiKey"`
	AWS    *awsCredentials `yaml:"aws"`
}

type awsCredentials struct {
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"accessKeyId"`
	SecretAccessKey string `yaml:"secretAccessKey"`
	SessionToken    string `yaml:"sessionToken"`
}

type permissionRule struct {
	Action   string `yaml:"action"`
	Resource string `yaml:"resource"`
	Effect   string `yaml:"effect"`
}

type tlsConfig struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

func defaultConfig() config {
	return config{
		Listen:              "127.0.0.1:8787",
		Agents:              []string{"build"},
		KeepAlive:           "15s",
		InteractionFallback: "reject",
	}
}

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

func (c config) isLoopback() bool {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validate runs every check in order, returning the first failure.
func (c config) validate() error {
	checks := []func() error{
		c.validateRequired,
		c.validateCredentials,
		c.validateAuth,
		c.validateLimits,
		c.validatePermissions,
		c.validateHTTP,
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (c config) validateRequired() error {
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
	}
	return nil
}

func (c config) validateCredentials() error {
	aws := c.Credentials.AWS
	if aws == nil {
		return nil
	}
	if aws.Region == "" || aws.AccessKeyID == "" || aws.SecretAccessKey == "" {
		return errors.New("aws credentials require region, accessKeyId and secretAccessKey")
	}
	return nil
}

func (c config) validateAuth() error {
	if c.Auth != nil {
		hasBasic := c.Auth.Username != "" && c.Auth.Password != ""
		if c.Auth.Bearer == "" && !hasBasic {
			return errors.New("auth requires bearer or a complete username + password")
		}
	}
	if !c.isLoopback() && !c.AllowInsecure && c.Auth == nil {
		return errors.New("refusing to bind a non-loopback address without auth; set auth.bearer, bind loopback, or allowInsecure")
	}
	return nil
}

func (c config) validateLimits() error {
	if c.ContextLimit < 0 || c.OutputLimit < 0 || c.SubagentDepth < 0 || c.SubscriberCapacity < 0 {
		return errors.New("limits must not be negative")
	}
	return nil
}

func (c config) validatePermissions() error {
	for _, rule := range c.InboundPermissions {
		switch schema.PermissionEffect(rule.Effect) {
		case schema.PermissionEffectAllow, schema.PermissionEffectDeny, schema.PermissionEffectAsk:
		default:
			return fmt.Errorf("inboundPermissions: unknown effect %q", rule.Effect)
		}
	}
	return nil
}

func (c config) validateHTTP() error {
	if c.InteractionFallback != "" && c.InteractionFallback != "reject" && c.InteractionFallback != "allow" {
		return errors.New("interactionFallback must be reject or allow")
	}
	if c.TLS != nil && (c.TLS.CertFile == "" || c.TLS.KeyFile == "") {
		return errors.New("tls requires both certFile and keyFile")
	}
	if _, err := c.keepAliveDuration(); err != nil {
		return err
	}
	if _, err := c.interactionTimeout(); err != nil {
		return err
	}
	return nil
}

func (c config) keepAliveDuration() (time.Duration, error) {
	if c.KeepAlive == "" {
		return 15 * time.Second, nil
	}
	duration, err := time.ParseDuration(c.KeepAlive)
	if err != nil {
		return 0, fmt.Errorf("keepAlive: %w", err)
	}
	return duration, nil
}

func (c config) interactionTimeout() (time.Duration, error) {
	if c.InteractionTimeout == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(c.InteractionTimeout)
	if err != nil {
		return 0, fmt.Errorf("interactionTimeout: %w", err)
	}
	if duration < 0 {
		return 0, errors.New("interactionTimeout must not be negative")
	}
	return duration, nil
}

func (c config) permissionRuleset() schema.PermissionRuleset {
	if len(c.InboundPermissions) == 0 {
		return nil
	}
	rules := make(schema.PermissionRuleset, 0, len(c.InboundPermissions))
	for _, rule := range c.InboundPermissions {
		rules = append(rules, schema.PermissionRule{
			Action:   rule.Action,
			Resource: rule.Resource,
			Effect:   schema.PermissionEffect(rule.Effect),
		})
	}
	return rules
}
