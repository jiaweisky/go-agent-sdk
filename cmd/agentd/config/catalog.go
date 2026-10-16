package config

import (
	"fmt"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// MediaConfig declares a model's media capability: input modalities and
// the sources (inline/url/file) it accepts. Input is sugar over
// ModelCapabilities.Input.
type MediaConfig struct {
	Input []string `yaml:"input"`
	// Forms lists, per modality (image/audio/video/file), the wire forms the model
	// accepts (inline/url/file).
	Forms map[string][]string `yaml:"forms"`
}

// ModelConfig is one per-model catalog entry.
type ModelConfig struct {
	// ID keys the catalog and names the model in agent configs. It is never
	// sent to the provider.
	ID string `yaml:"id"`
	// Model is the wire model name sent to the provider; it is required and
	// never falls back to ID.
	Model string `yaml:"model"`
	// ProviderID keys credentials; empty means Protocol.
	ProviderID string `yaml:"providerID"`
	// Protocol selects the wire route (e.g. openai-compatible-chat).
	Protocol string `yaml:"protocol"`
	// ModelBaseURL overrides the route's default endpoint.
	ModelBaseURL string `yaml:"modelBaseURL"`
	// InputModalities is shorthand for Media.Input.
	InputModalities []string `yaml:"inputModalities"`
	// Media declares modalities and accepted sources.
	Media *MediaConfig `yaml:"media"`
	// Credentials is the model's provider credential.
	Credentials Credentials `yaml:"credentials"`
	// Headers are extra request headers sent with every model request (e.g. the
	// `anthropic-beta` header Anthropic Files API references require).
	Headers map[string]string `yaml:"headers"`
	// ContextLimit / OutputLimit are the model's token limits.
	ContextLimit int `yaml:"contextLimit"`
	OutputLimit  int `yaml:"outputLimit"`
}

// providerID returns the credential key: ProviderID, or the Protocol.
func (m ModelConfig) providerID() schema.ProviderID {
	if m.ProviderID != "" {
		return schema.ProviderID(m.ProviderID)
	}
	return schema.ProviderID(m.Protocol)
}

// inputs returns the declared input modalities (Media.Input wins over the
// InputModalities shorthand).
func (m ModelConfig) inputs() []string {
	if m.Media != nil && len(m.Media.Input) > 0 {
		return m.Media.Input
	}
	return m.InputModalities
}

// baseURL returns the model's endpoint: an explicit ModelBaseURL, a
// region-derived Amazon Bedrock endpoint when only AWS credentials are set, or
// "" to use the protocol's default endpoint.
func (m ModelConfig) baseURL() string {
	if m.ModelBaseURL != "" {
		return m.ModelBaseURL
	}
	if m.Protocol == "bedrock-converse" && m.Credentials.AWS != nil && m.Credentials.AWS.Region != "" {
		return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", m.Credentials.AWS.Region)
	}
	return ""
}

// Validate checks one catalog entry's identity, credential and media
// declaration.
func (m ModelConfig) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("model entry requires an id")
	}
	if m.Model == "" {
		return fmt.Errorf("model %q requires a model (the wire model name sent to the provider)", m.ID)
	}
	if m.Protocol == "" {
		return fmt.Errorf("model %q requires a protocol", m.ID)
	}
	if aws := m.Credentials.AWS; aws != nil {
		if aws.Region == "" || aws.AccessKeyID == "" || aws.SecretAccessKey == "" {
			return fmt.Errorf("model %q: aws credentials require region, accessKeyId and secretAccessKey", m.ID)
		}
	}
	if err := ValidateInputModalities(m.inputs()); err != nil {
		return err
	}
	if m.Media != nil {
		if err := ValidateMediaForms(m.Media.Forms); err != nil {
			return err
		}
	}
	return nil
}

// Catalog resolves model metadata and credentials per model id. The first
// entry is the default; an unknown id falls back to it.
type Catalog struct {
	entries    map[string]ModelConfig
	order      []string
	byProvider map[schema.ProviderID]schema.Credential
	defaultID  string
}

// NewCatalog builds a catalog from a default entry plus additional models.
func NewCatalog(defaultEntry ModelConfig, models []ModelConfig) (Catalog, error) {
	catalog := Catalog{
		entries:    make(map[string]ModelConfig),
		byProvider: make(map[schema.ProviderID]schema.Credential),
	}
	for _, entry := range append([]ModelConfig{defaultEntry}, models...) {
		if err := entry.Validate(); err != nil {
			return Catalog{}, err
		}
		if _, dup := catalog.entries[entry.ID]; dup {
			return Catalog{}, fmt.Errorf("duplicate model id %q", entry.ID)
		}
		catalog.entries[entry.ID] = entry
		catalog.order = append(catalog.order, entry.ID)
		catalog.recordCredential(entry.providerID(), toSchemaCredential(entry.Credentials))
	}
	if len(catalog.order) == 0 {
		return Catalog{}, fmt.Errorf("catalog requires at least one model")
	}
	catalog.defaultID = catalog.order[0]
	return catalog, nil
}

// recordCredential keeps the first non-empty credential seen for a provider, so
// a later entry sharing the provider cannot blank it.
func (c *Catalog) recordCredential(provider schema.ProviderID, credential schema.Credential) {
	existing, seen := c.byProvider[provider]
	if !seen || (credentialEmpty(existing) && !credentialEmpty(credential)) {
		c.byProvider[provider] = credential
	}
}

// DefaultID is the catalog's default model id.
func (c Catalog) DefaultID() string { return c.defaultID }

// IDs returns the catalog's model ids in registration order.
func (c Catalog) IDs() []string { return append([]string(nil), c.order...) }

// ModelInfo resolves a model id to SDK metadata, applying the entry's provider
// id, endpoint, token limits, modalities and media sources. An unknown id falls
// back to the default entry.
func (c Catalog) ModelInfo(id string) (schema.ModelInfo, error) {
	entry, ok := c.entries[id]
	if !ok {
		entry = c.entries[c.defaultID]
	}
	info, err := agent.NewModelInfo(
		schema.ProtocolID(entry.Protocol),
		entry.Model,
		entry.baseURL(),
		schema.ModelLimit{Context: entry.ContextLimit, Output: entry.OutputLimit},
	)
	if err != nil {
		return schema.ModelInfo{}, err
	}
	// ID is the catalog/agent-facing id; the wire model name lives in API.ID
	// (set by NewModelInfo from the entry's model).
	info.ID = schema.ModelID(entry.ID)
	info.ProviderID = entry.providerID()
	if inputs := entry.inputs(); len(inputs) > 0 {
		info.Capabilities.Input = inputs
	}
	if entry.Media != nil && len(entry.Media.Forms) > 0 {
		info.Capabilities.Media = &schema.ModelMediaCapability{Forms: entry.Media.Forms}
	}
	for name, value := range entry.Headers {
		info.Request.Headers[name] = value
	}
	return info, nil
}

// Credentials returns the credential for a provider id.
func (c Catalog) Credentials(providerID schema.ProviderID) (schema.Credential, error) {
	if credential, ok := c.byProvider[providerID]; ok {
		return credential, nil
	}
	return schema.Credential{}, fmt.Errorf("unknown provider: %s", providerID)
}

// Subset returns a catalog restricted to the given ids (registration order
// preserved, duplicates dropped), with defaultID as its default. An empty ids
// returns the whole catalog; a defaultID not present is an error.
func (c Catalog) Subset(ids []string, defaultID string) (Catalog, error) {
	if defaultID != "" {
		if _, ok := c.entries[defaultID]; !ok {
			return Catalog{}, fmt.Errorf("default model %q is not in the catalog", defaultID)
		}
	}
	if len(ids) == 0 {
		out := c
		if defaultID != "" {
			out.defaultID = defaultID
		}
		return out, nil
	}
	out := Catalog{entries: make(map[string]ModelConfig, len(ids)), byProvider: c.byProvider}
	for _, id := range ids {
		entry, ok := c.entries[id]
		if !ok {
			return Catalog{}, fmt.Errorf("unknown model id %q", id)
		}
		if _, dup := out.entries[id]; dup {
			continue
		}
		out.entries[id] = entry
		out.order = append(out.order, id)
	}
	out.defaultID = out.order[0]
	if defaultID != "" {
		if _, ok := out.entries[defaultID]; !ok {
			return Catalog{}, fmt.Errorf("default model %q is not in the server's models", defaultID)
		}
		out.defaultID = defaultID
	}
	return out, nil
}

// credentialEmpty reports whether a credential carries no secret.
func credentialEmpty(credential schema.Credential) bool {
	return credential.APIKey == "" && credential.AWS == nil
}

// ModelInfos resolves every catalog model's metadata for the discovery APIs.
func ModelInfos(catalog Catalog) ([]schema.ModelInfo, error) {
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
