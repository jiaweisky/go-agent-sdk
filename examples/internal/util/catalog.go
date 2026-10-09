package util

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// Pricing is a model's per-million-token usage pricing.
type Pricing struct {
	CacheHit  float64
	CacheMiss float64
	Output    float64
}

// ModelEntry describes one selectable model: how to reach it (provider,
// protocol, endpoint, credential env var), the token limits the SDK uses, and
// optionally its usage pricing.
type ModelEntry struct {
	ID string
	// Model is the model name sent to the provider; empty means ID. It lets one
	// underlying model be offered under several protocol-specific catalog ids.
	Model      string
	ProviderID schema.ProviderID
	Protocol   schema.ProtocolID
	BaseURL    string
	APIKeyEnv  string
	Context    int
	Output     int
	Pricing    *Pricing
	Default    bool
}

// Catalog is an in-code model manifest. A model is available only when its
// APIKeyEnv is set, so one provider key exposes only that provider's models.
type Catalog struct {
	byID       map[string]ModelEntry
	byProvider map[schema.ProviderID]ModelEntry
	ids        []string
}

// NewCatalog indexes entries by id and by provider.
func NewCatalog(entries []ModelEntry) Catalog {
	c := Catalog{
		byID:       make(map[string]ModelEntry, len(entries)),
		byProvider: make(map[schema.ProviderID]ModelEntry, len(entries)),
	}
	for _, entry := range entries {
		c.byID[entry.ID] = entry
		if _, seen := c.byProvider[entry.ProviderID]; !seen {
			c.byProvider[entry.ProviderID] = entry
		}
		c.ids = append(c.ids, entry.ID)
	}
	sort.Strings(c.ids)
	return c
}

// Entry returns the catalog entry for an id.
func (c Catalog) Entry(id string) (ModelEntry, bool) {
	entry, ok := c.byID[id]
	return entry, ok
}

// Available reports whether the model's API key is present.
func (c Catalog) Available(id string) bool {
	entry, ok := c.byID[id]
	return ok && os.Getenv(entry.APIKeyEnv) != ""
}

// AvailableIDs lists the selectable model ids, sorted.
func (c Catalog) AvailableIDs() []string {
	ids := make([]string, 0, len(c.ids))
	for _, id := range c.ids {
		if c.Available(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// DefaultID is the model used when none is chosen: the entry marked Default
// when available, otherwise the first available model, otherwise "".
func (c Catalog) DefaultID() string {
	for _, id := range c.ids {
		if c.byID[id].Default && c.Available(id) {
			return id
		}
	}
	if ids := c.AvailableIDs(); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ModelInfo resolves a model id to its metadata, including the per-model
// protocol and endpoint.
func (c Catalog) ModelInfo(id string) (schema.ModelInfo, error) {
	entry, ok := c.byID[id]
	if !ok {
		return schema.ModelInfo{}, fmt.Errorf("unknown model: %s", id)
	}
	limit := schema.ModelLimit{Context: entry.Context, Output: entry.Output}
	info, err := agent.NewModelInfo(entry.Protocol, id, entry.BaseURL, limit)
	if err != nil {
		return info, err
	}
	info.ProviderID = entry.ProviderID
	// ID selects the catalog entry; Model is what the provider is asked for.
	if entry.Model != "" {
		modelID := schema.ModelID(entry.Model)
		info.API.ID = &modelID
	}
	return info, nil
}

// Credentials returns the provider's API key from its configured env var.
func (c Catalog) Credentials(providerID schema.ProviderID) (schema.Credential, error) {
	entry, ok := c.byProvider[providerID]
	if !ok {
		return schema.Credential{}, fmt.Errorf("unknown provider: %s", providerID)
	}
	key := os.Getenv(entry.APIKeyEnv)
	if key == "" {
		return schema.Credential{}, fmt.Errorf("provider %s needs %s", providerID, entry.APIKeyEnv)
	}
	return schema.Credential{APIKey: key}, nil
}

// Pricing returns the model's usage pricing, when known.
func (c Catalog) Pricing(id string) (Pricing, bool) {
	entry, ok := c.byID[id]
	if !ok || entry.Pricing == nil {
		return Pricing{}, false
	}
	return *entry.Pricing, true
}

// Option injects the catalog's model metadata and credentials.
func (c Catalog) Option() Option {
	return func(opts *agent.ClientOptions) {
		opts.ModelInfo = c.ModelInfo
		opts.Credentials = c.Credentials
	}
}

// WithModel sets the client's default model.
func WithModel(id string) Option {
	return func(opts *agent.ClientOptions) {
		opts.Model = id
	}
}

// DefaultCatalog is the shared example model manifest. Only models whose API
// key is present are offered.
var DefaultCatalog = NewCatalog([]ModelEntry{
	{
		ID: "deepseek-v4-flash", ProviderID: "deepseek",
		Protocol: "openai-compatible-chat", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Context: 1_000_000, Output: 384_000,
		Pricing: &Pricing{CacheHit: 0.04, CacheMiss: 2.0, Output: 8.0}, Default: true,
	},
	{
		ID: "deepseek-v4-pro", ProviderID: "deepseek",
		Protocol: "openai-compatible-chat", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Context: 1_000_000, Output: 384_000,
		Pricing: &Pricing{CacheHit: 0.30, CacheMiss: 9.0, Output: 27.0},
	},
	// The same DeepSeek model over each protocol it exposes, so one
	// DEEPSEEK_API_KEY reaches the three routes the SDK can speak.
	{
		ID: "deepseek-flash", ProviderID: "deepseek",
		Protocol: "openai-compatible-chat", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Model: "deepseek-flash",
		Context: 1_000_000, Output: 384_000,
	},
	{
		ID: "deepseek-flash-responses", ProviderID: "deepseek",
		Protocol: "openai-responses", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Model: "deepseek-flash",
		Context: 1_000_000, Output: 384_000,
	},
	{
		ID: "deepseek-flash-anthropic", ProviderID: "deepseek",
		Protocol: "anthropic-messages", BaseURL: "https://api.deepseek.com/anthropic",
		APIKeyEnv: "DEEPSEEK_API_KEY", Model: "deepseek-flash",
		Context: 1_000_000, Output: 384_000,
	},
	{
		ID: "gpt-5", ProviderID: "openai",
		Protocol: "openai-responses", APIKeyEnv: "OPENAI_API_KEY",
		Context: 400_000, Output: 128_000,
	},
	{
		ID: "claude-sonnet-4", ProviderID: "anthropic",
		Protocol: "anthropic-messages", APIKeyEnv: "ANTHROPIC_API_KEY",
		Context: 200_000, Output: 64_000,
	},
})

// providerKeyEnvs lists the distinct APIKeyEnv values, in catalog id order.
func (c Catalog) providerKeyEnvs() []string {
	seen := make(map[string]bool, len(c.ids))
	var envs []string
	for _, id := range c.ids {
		env := c.byID[id].APIKeyEnv
		if env != "" && !seen[env] {
			seen[env] = true
			envs = append(envs, env)
		}
	}
	return envs
}

// DefaultModel is the model an example starts with: the catalog default when a
// provider key is available, otherwise an error prompting for one.
func (c Catalog) DefaultModel() (string, error) {
	if id := c.DefaultID(); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no model available: set a provider API key (%s)",
		strings.Join(c.providerKeyEnvs(), ", "))
}
