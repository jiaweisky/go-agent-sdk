package config

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
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

// ProviderKey returns the credential key: ProviderID, or the Protocol.
func (m ModelConfig) ProviderKey() schema.ProviderID {
	if m.ProviderID != "" {
		return schema.ProviderID(m.ProviderID)
	}
	return schema.ProviderID(m.Protocol)
}

// EffectiveInputs returns the declared input modalities (Media.Input wins over
// the InputModalities shorthand).
func (m ModelConfig) EffectiveInputs() []string {
	if m.Media != nil && len(m.Media.Input) > 0 {
		return m.Media.Input
	}
	return m.InputModalities
}

// Endpoint returns the model's endpoint: an explicit ModelBaseURL, a
// region-derived Amazon Bedrock endpoint when only AWS credentials are set, or
// "" to use the protocol's default endpoint.
func (m ModelConfig) Endpoint() string {
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
	if _, ok := protocols.LookupProtocol(llmschema.ProtocolID(m.Protocol)); !ok {
		return fmt.Errorf("model %q: unknown protocol %q", m.ID, m.Protocol)
	}
	if aws := m.Credentials.AWS; aws != nil {
		if aws.Region == "" || aws.AccessKeyID == "" || aws.SecretAccessKey == "" {
			return fmt.Errorf("model %q: aws credentials require region, accessKeyId and secretAccessKey", m.ID)
		}
	}
	if err := ValidateInputModalities(m.EffectiveInputs()); err != nil {
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
		catalog.recordCredential(entry.ProviderKey(), toSchemaCredential(entry.Credentials))
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

// Entry returns a model entry and whether the id exists.
func (c Catalog) Entry(id string) (ModelConfig, bool) {
	entry, ok := c.entries[id]
	return entry, ok
}

// DefaultEntry returns the catalog's default model entry.
func (c Catalog) DefaultEntry() (ModelConfig, bool) {
	entry, ok := c.entries[c.defaultID]
	return entry, ok
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
