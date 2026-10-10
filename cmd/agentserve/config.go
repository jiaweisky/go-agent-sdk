package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/mcp"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// config is the agentserve configuration, assembled from defaults, then a YAML
// file, then CLI flags. No environment variables are read.
type config struct {
	Listen           string                    `yaml:"listen"`
	Root             string                    `yaml:"root"`
	WorkingDirectory string                    `yaml:"workingDirectory"`
	ConfigDirs       []string                  `yaml:"configDirs"`
	Model            string                    `yaml:"model"`
	Protocol         string                    `yaml:"protocol"`
	ModelBaseURL     string                    `yaml:"modelBaseURL"`
	InputModalities  []string                  `yaml:"inputModalities"`
	Models           []serveconfig.ModelConfig `yaml:"models"`
	Credentials      serveconfig.Credentials   `yaml:"credentials"`
	// MediaUpload configures the generic Files API uploader for the HTTP
	// upload endpoint and inline→file adaptation. Nil disables it.
	MediaUpload      *serveconfig.MediaUploadConfig `yaml:"mediaUpload"`
	ContextLimit     int                            `yaml:"contextLimit"`
	OutputLimit      int                            `yaml:"outputLimit"`
	SubagentDepth    int                            `yaml:"subagentDepth"`
	TitleAuto        bool                           `yaml:"titleAuto"`
	DropErroredTurns bool                           `yaml:"dropErroredTurns"`
	Agents           []string                       `yaml:"agents"`

	Auth                *authConfig                  `yaml:"auth"`
	InboundPermissions  []serveconfig.PermissionRule `yaml:"inboundPermissions"`
	InteractionTimeout  string                       `yaml:"interactionTimeout"`
	InteractionFallback string                       `yaml:"interactionFallback"`
	// Questions selects the question tool's policy: "park" (default) holds the
	// turn until the HTTP client answers; "decline" rejects questions, ending
	// the turn with the dismissal instead of parking it forever.
	Questions          string   `yaml:"questions"`
	SubscriberCapacity int      `yaml:"subscriberCapacity"`
	KeepAlive          string   `yaml:"keepAlive"`
	PublicRoutes       []string `yaml:"publicRoutes"`
	CORS               []string `yaml:"cors"`

	TLS           *serveconfig.TLS `yaml:"tls"`
	AllowInsecure bool             `yaml:"allowInsecure"`

	MCP        []mcpConfig       `yaml:"mcp"`
	References []referenceConfig `yaml:"references"`
}

// referenceConfig configures one named reference source (local path or git
// repository) injected into the system context.
type referenceConfig struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"` // local | git
	Path        string `yaml:"path"`
	Repository  string `yaml:"repository"`
	Branch      string `yaml:"branch"`
	Description string `yaml:"description"`
	Hidden      *bool  `yaml:"hidden"`
}

// referenceSources converts the configured references for the client.
func (c config) referenceSources() (map[string]core.ReferenceSource, error) {
	if len(c.References) == 0 {
		return nil, nil
	}
	out := make(map[string]core.ReferenceSource, len(c.References))
	for _, ref := range c.References {
		if ref.Name == "" {
			return nil, errors.New("references: each requires a name")
		}
		switch ref.Type {
		case "local":
			if ref.Path == "" {
				return nil, fmt.Errorf("reference %s: local requires path", ref.Name)
			}
			out[ref.Name] = schema.ReferenceLocalSource{
				Type: "local", Path: schema.AbsolutePath(ref.Path),
				Description: optionalString(ref.Description), Hidden: ref.Hidden,
			}
		case "git":
			if ref.Repository == "" {
				return nil, fmt.Errorf("reference %s: git requires repository", ref.Name)
			}
			source := schema.ReferenceGitSource{
				Type: "git", Repository: ref.Repository,
				Description: optionalString(ref.Description), Hidden: ref.Hidden,
			}
			if ref.Branch != "" {
				branch := ref.Branch
				source.Branch = &branch
			}
			out[ref.Name] = source
		default:
			return nil, fmt.Errorf("reference %s: unknown type %q (local or git)", ref.Name, ref.Type)
		}
	}
	return out, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// mcpConfig configures one MCP server (local stdio or remote StreamableHTTP).
type mcpConfig struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Command     []string          `yaml:"command"`
	Cwd         string            `yaml:"cwd"`
	Environment map[string]string `yaml:"environment"`
	URL         string            `yaml:"url"`
	Headers     map[string]string `yaml:"headers"`
	Enabled     *bool             `yaml:"enabled"`
	Timeout     string            `yaml:"timeout"`
}

type authConfig struct {
	Bearer   string `yaml:"bearer"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// mcpServers converts the configured MCP servers for the client.
func (c config) mcpServers() ([]agent.MCPConfig, error) {
	if len(c.MCP) == 0 {
		return nil, nil
	}
	result := make([]agent.MCPConfig, 0, len(c.MCP))
	for _, server := range c.MCP {
		timeout, err := parseOptionalDuration(server.Timeout)
		if err != nil {
			return nil, fmt.Errorf("mcp %s: timeout: %w", server.Name, err)
		}
		result = append(result, agent.MCPConfig{
			Name: server.Name,
			Server: mcp.ServerConfig{
				Type:        server.Type,
				Command:     server.Command,
				Cwd:         server.Cwd,
				Environment: server.Environment,
				URL:         server.URL,
				Headers:     server.Headers,
				Enabled:     server.Enabled,
				Timeout:     timeout,
			},
		})
	}
	return result, nil
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

func defaultConfig() config {
	return config{
		Listen:              "127.0.0.1:8787",
		Agents:              []string{"build"},
		KeepAlive:           "15s",
		InteractionFallback: "reject",
		// A remote client cannot run the interactive revert dance, so the server
		// self-heals by default: a turn that ends in error is excluded from later
		// contexts. Set dropErroredTurns: false for opencode-CLI-style behavior.
		DropErroredTurns: true,
	}
}

// validate runs every check in order, returning the first failure.
func (c config) validate() error {
	checks := []func() error{
		c.validateRequired,
		c.validateCredentials,
		c.validateAuth,
		c.validateLimits,
		c.validateModalities,
		c.validatePermissions,
		c.validateMCP,
		c.validateReferences,
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
	case len(c.Agents) == 0:
		return errors.New("at least one agent is required")
	}
	// A default entry (top-level model) or a catalog-only `models:` list.
	if c.Model == "" && len(c.Models) == 0 {
		return errors.New("model is required")
	}
	if c.Model != "" {
		if c.Protocol == "" {
			return errors.New("protocol is required")
		}
		if c.Credentials.APIKey == "" && c.Credentials.AWS == nil {
			return errors.New("credentials must set apiKey or aws")
		}
	}
	return nil
}

// modelCatalog builds the per-model catalog from the default entry + `models:`.
func (c config) modelCatalog() (serveconfig.Catalog, error) {
	if c.Model == "" && len(c.Models) > 0 {
		return serveconfig.NewCatalog(c.Models[0], c.Models[1:])
	}
	return serveconfig.NewCatalog(serveconfig.ModelConfig{
		ID:              c.Model,
		Model:           c.Model,
		Protocol:        c.Protocol,
		ModelBaseURL:    c.ModelBaseURL,
		InputModalities: c.InputModalities,
		Credentials:     c.Credentials,
		ContextLimit:    c.ContextLimit,
		OutputLimit:     c.OutputLimit,
	}, c.Models)
}

// modelInfos resolves every catalog model's metadata for GET /api/model.
func (c config) modelInfos() ([]schema.ModelInfo, error) {
	catalog, err := c.modelCatalog()
	if err != nil {
		return nil, err
	}
	ids := catalog.IDs()
	infos := make([]schema.ModelInfo, 0, len(ids))
	for _, id := range ids {
		info, err := catalog.ModelInfo(id)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// mediaUploader returns a per-provider uploader resolver, or nil when no
// generic uploader is configured.
func (c config) mediaUploader() func(schema.ProviderID) schema.MediaUploader {
	if c.MediaUpload == nil {
		return nil
	}
	uploader := c.MediaUpload.Uploader()
	return func(schema.ProviderID) schema.MediaUploader { return uploader }
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
		// The basic-auth username defaults to "opencode", so a password alone
		// selects basic auth.
		if c.Auth.Bearer == "" && c.Auth.Password == "" {
			return errors.New("auth requires bearer or a password (username defaults to opencode)")
		}
	}
	if !serveconfig.IsLoopback(c.Listen) && !c.AllowInsecure && c.Auth == nil {
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

func (c config) validateModalities() error {
	if err := serveconfig.ValidateInputModalities(c.InputModalities); err != nil {
		return err
	}
	for _, model := range c.Models {
		if err := model.Validate(); err != nil {
			return fmt.Errorf("models: %w", err)
		}
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

func (c config) validateMCP() error {
	for _, server := range c.MCP {
		switch {
		case server.Name == "":
			return errors.New("mcp: each server requires a name")
		case server.Type == "":
			return fmt.Errorf("mcp %s: type is required (local or remote)", server.Name)
		case server.Type == "local" && len(server.Command) == 0:
			return fmt.Errorf("mcp %s: local requires command", server.Name)
		case server.Type == "remote" && server.URL == "":
			return fmt.Errorf("mcp %s: remote requires url", server.Name)
		}
		if _, err := parseOptionalDuration(server.Timeout); err != nil {
			return fmt.Errorf("mcp %s: timeout: %w", server.Name, err)
		}
	}
	return nil
}

func (c config) validateReferences() error {
	_, err := c.referenceSources()
	return err
}

func (c config) validateHTTP() error {
	if c.InteractionFallback != "" && c.InteractionFallback != "reject" && c.InteractionFallback != "allow" {
		return errors.New("interactionFallback must be reject or allow")
	}
	switch c.Questions {
	case "", "park", "decline":
	default:
		return fmt.Errorf("questions must be park or decline, got %q", c.Questions)
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
