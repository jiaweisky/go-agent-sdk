package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/serveconfig"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// config is the a2aserve configuration. It is assembled from built-in defaults,
// then a YAML file, then CLI flags (flags win). No environment variables are
// read.
type config struct {
	Listen           string                    `yaml:"listen"`
	BaseURL          string                    `yaml:"baseURL"`
	Root             string                    `yaml:"root"`
	WorkingDirectory string                    `yaml:"workingDirectory"`
	ConfigDirs       []string                  `yaml:"configDirs"`
	Model            string                    `yaml:"model"`
	Protocol         string                    `yaml:"protocol"`
	ModelBaseURL     string                    `yaml:"modelBaseURL"`
	InputModalities  []string                  `yaml:"inputModalities"`
	Models           []serveconfig.ModelConfig `yaml:"models"`
	InboundMedia     string                    `yaml:"inboundMedia"`
	// MediaUpload configures the generic Files API uploader for inline→file
	// adaptation. Nil disables it.
	MediaUpload        *serveconfig.MediaUploadConfig `yaml:"mediaUpload"`
	DropErroredTurns   bool                           `yaml:"dropErroredTurns"`
	Credentials        serveconfig.Credentials        `yaml:"credentials"`
	Agents             []string                       `yaml:"agents"`
	Cards              map[string]cardConfig          `yaml:"cards"`
	ContextLimit       int                            `yaml:"contextLimit"`
	OutputLimit        int                            `yaml:"outputLimit"`
	InboundPermissions []serveconfig.PermissionRule   `yaml:"inboundPermissions"`
	KeepAlive          string                         `yaml:"keepAlive"`
	Auth               *authConfig                    `yaml:"auth"`
	TLS                *serveconfig.TLS               `yaml:"tls"`
	// AllowInsecure permits binding a non-loopback address without auth. It is
	// an explicit opt-out of the safe default.
	AllowInsecure bool `yaml:"allowInsecure"`
}

// cardConfig overrides an exposed agent's AgentCard.
type cardConfig struct {
	Name             string          `yaml:"name"`
	Description      string          `yaml:"description"`
	Version          string          `yaml:"version"`
	DocumentationURL string          `yaml:"documentationUrl"`
	IconURL          string          `yaml:"iconUrl"`
	Provider         *providerConfig `yaml:"provider"`
	Skills           []skillConfig   `yaml:"skills"`
}

// providerConfig identifies the organization offering the agent.
type providerConfig struct {
	Organization string `yaml:"organization"`
	URL          string `yaml:"url"`
}

// skillConfig advertises one capability. ID must name a real capability of the
// agent (a tool, subagent or skill); the rest is the human-readable layer.
type skillConfig struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Examples    []string `yaml:"examples"`
	InputModes  []string `yaml:"inputModes"`
	OutputModes []string `yaml:"outputModes"`
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
		// A remote A2A client cannot run the interactive revert dance, so the
		// server self-heals by default: a turn that ends in error is excluded from
		// later contexts. Set dropErroredTurns: false to opt out.
		DropErroredTurns: true,
	}
}

// validate reports configuration that cannot produce a running server.
func (c config) validate() error {
	switch {
	case c.Root == "":
		return errors.New("root is required (the SDK state directory)")
	case len(c.Agents) == 0:
		return errors.New("at least one agent is required")
	case c.Auth != nil && c.Auth.Bearer == "":
		return errors.New("auth.bearer is required when auth is set")
	case !serveconfig.IsLoopback(c.Listen) && !c.AllowInsecure && c.Auth == nil:
		return errors.New(
			"refusing to bind a non-loopback address without auth; set auth.bearer, bind loopback, or allowInsecure",
		)
	}
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
	if err := serveconfig.ValidateInputModalities(c.InputModalities); err != nil {
		return err
	}
	for _, model := range c.Models {
		if err := model.Validate(); err != nil {
			return fmt.Errorf("models: %w", err)
		}
	}
	switch c.InboundMedia {
	case "", "spool", "inline":
	default:
		return fmt.Errorf("inboundMedia must be spool or inline, got %q", c.InboundMedia)
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

// modelInfos resolves every catalog model's metadata (for card modes).
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

// mediaUploader returns a per-provider uploader resolver, or nil.
func (c config) mediaUploader() func(schema.ProviderID) schema.MediaUploader {
	if c.MediaUpload == nil {
		return nil
	}
	uploader := c.MediaUpload.Uploader()
	return func(schema.ProviderID) schema.MediaUploader { return uploader }
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
