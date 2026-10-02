package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/examples/internal/util"
	"github.com/user/go-agent-sdk/schema"
)

// deepseekPricing is the per-million-token peak price in CNY.
type deepseekPricing struct {
	cacheHit  float64
	cacheMiss float64
	output    float64
}

// Peak-hour rates from https://api-docs.deepseek.com/zh-cn/quick_start/pricing.
// Off-peak is half price.
var (
	deepseekFlashPricing = deepseekPricing{cacheHit: 0.04, cacheMiss: 2.0, output: 8.0}
	deepseekProPricing   = deepseekPricing{cacheHit: 0.30, cacheMiss: 9.0, output: 27.0}
)

// modelEntry describes one selectable model: how to reach it (provider,
// protocol, endpoint, credential env var), the token limits the SDK uses, and
// optionally its usage pricing.
type modelEntry struct {
	ID         string
	ProviderID schema.ProviderID
	// ProviderType is the AGENT_PROVIDER value this entry follows. The entry
	// that matches the configured provider authenticates with AGENT_API_KEY and
	// AGENT_BASE_URL; other entries use their own environment variables.
	ProviderType string
	Protocol     schema.ProtocolID
	BaseURL      string
	APIKeyEnv    string
	Context      int
	Output       int
	Pricing      *deepseekPricing
	Default      bool
}

// modelCatalog is the in-code manifest of models the chat can use. Only models
// whose API key is present are offered.
var modelCatalog = []modelEntry{
	{
		ID: "deepseek-v4-flash", ProviderID: "deepseek", ProviderType: "openai-compatible",
		Protocol: "openai-compatible-chat", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Context: 1_000_000, Output: 384_000,
		Pricing: &deepseekFlashPricing, Default: true,
	},
	{
		ID: "deepseek-v4-pro", ProviderID: "deepseek", ProviderType: "openai-compatible",
		Protocol: "openai-compatible-chat", BaseURL: "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY", Context: 1_000_000, Output: 384_000,
		Pricing: &deepseekProPricing,
	},
	{
		ID: "gpt-5", ProviderID: "openai", ProviderType: "openai",
		Protocol: "openai-responses", APIKeyEnv: "OPENAI_API_KEY",
		Context: 400_000, Output: 128_000,
	},
	{
		ID: "claude-sonnet-4", ProviderID: "anthropic", ProviderType: "anthropic",
		Protocol: "anthropic-messages", APIKeyEnv: "ANTHROPIC_API_KEY",
		Context: 200_000, Output: 64_000,
	},
}

// catalog resolves entries by id and provider credentials by provider id.
type catalog struct {
	byID       map[string]modelEntry
	byProvider map[schema.ProviderID]modelEntry
	ids        []string
}

func newCatalog(entries []modelEntry) catalog {
	c := catalog{
		byID:       make(map[string]modelEntry, len(entries)),
		byProvider: make(map[schema.ProviderID]modelEntry, len(entries)),
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

// models is the catalog the chat runs with.
var models = newCatalog(modelCatalog)

// envProvider is the configured AGENT_PROVIDER.
func envProvider() string { return envOr("AGENT_PROVIDER", "openai") }

// envProviderID is the configured AGENT_PROVIDER_ID, if any.
func envProviderID() string { return os.Getenv("AGENT_PROVIDER_ID") }

// entryIsConfigured reports whether an entry is the provider configured through
// the AGENT_* environment.
func entryIsConfigured(entry modelEntry) bool {
	if entry.ProviderType != envProvider() {
		return false
	}
	id := envProviderID()
	return id == "" || string(entry.ProviderID) == id
}

// entryKey is an entry's API key: the generic AGENT_API_KEY for the configured
// provider, otherwise the entry's own provider environment variable.
func entryKey(entry modelEntry) string {
	if entryIsConfigured(entry) {
		if key := os.Getenv("AGENT_API_KEY"); key != "" {
			return key
		}
	}
	return os.Getenv(entry.APIKeyEnv)
}

// entryBaseURL uses AGENT_BASE_URL for the configured provider.
func entryBaseURL(entry modelEntry) string {
	if entryIsConfigured(entry) {
		if url := os.Getenv("AGENT_BASE_URL"); url != "" {
			return url
		}
	}
	return entry.BaseURL
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// available reports whether the model's provider key is present.
func (c catalog) available(id string) bool {
	entry, ok := c.byID[id]
	if !ok {
		return false
	}
	return entryKey(entry) != ""
}

// availableIDs lists the selectable model ids, sorted.
func (c catalog) availableIDs() []string {
	ids := make([]string, 0, len(c.ids))
	for _, id := range c.ids {
		if c.available(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// defaultID is the model used when none is chosen: the entry marked Default
// when available, otherwise the first available model, otherwise "".
func (c catalog) defaultID() string {
	for _, id := range c.ids {
		if c.byID[id].Default && c.available(id) {
			return id
		}
	}
	if ids := c.availableIDs(); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// modelInfo resolves a model id to its metadata, including the per-model
// protocol and endpoint.
func (c catalog) modelInfo(id string) (schema.ModelInfo, error) {
	entry, ok := c.byID[id]
	if !ok {
		return schema.ModelInfo{}, fmt.Errorf("unknown model: %s", id)
	}
	limit := schema.ModelLimit{Context: entry.Context, Output: entry.Output}
	info, err := agent.NewModelInfo(entry.Protocol, id, entryBaseURL(entry), limit)
	if err != nil {
		return info, err
	}
	info.ProviderID = entry.ProviderID
	return info, nil
}

// credentials returns the provider's API key from its configured env var.
func (c catalog) credentials(providerID schema.ProviderID) (schema.Credential, error) {
	entry, ok := c.byProvider[providerID]
	if !ok {
		return schema.Credential{}, fmt.Errorf("unknown provider: %s", providerID)
	}
	key := entryKey(entry)
	if key == "" {
		return schema.Credential{}, fmt.Errorf("provider %s needs %s", providerID, entry.APIKeyEnv)
	}
	return schema.Credential{APIKey: key}, nil
}

// pricing returns the model's usage pricing, when known.
func (c catalog) pricing(id string) (deepseekPricing, bool) {
	entry, ok := c.byID[id]
	if !ok || entry.Pricing == nil {
		return deepseekPricing{}, false
	}
	return *entry.Pricing, true
}

// withCatalog injects the catalog's model metadata and credentials.
func withCatalog(c catalog) util.Option {
	return func(opts *agent.ClientOptions) {
		opts.ModelInfo = c.modelInfo
		opts.Credentials = c.credentials
	}
}

// withDefaultModel sets the client's default model.
func withDefaultModel(id string) util.Option {
	return func(opts *agent.ClientOptions) {
		opts.Model = id
	}
}

// resolveDefaultModel picks the model to start with: the persisted selection,
// then AGENT_MODEL, then the catalog default.
func resolveDefaultModel(persisted string) string {
	if persisted != "" && models.available(persisted) {
		return persisted
	}
	if id := os.Getenv("AGENT_MODEL"); id != "" && models.available(id) {
		return id
	}
	return models.defaultID()
}

// modelCommand handles "/model" (list) and "/model <id>" (switch). It changes
// the active session's model in place, so the choice applies from the next turn
// without starting a new session.
func (c *chat) modelCommand(input string) {
	name := commandArgument(input, "/model")
	if name == "" {
		c.writeModels()
		return
	}
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	entry, ok := models.byID[name]
	if !ok || !models.available(name) {
		c.console.write(fmt.Sprintf("unknown or unavailable model: %s\n", name))
		return
	}
	ref := schema.ModelRef{ID: schema.ModelID(name), ProviderID: entry.ProviderID}
	if err := c.session.SetModel(ref); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.setModel(name)
	c.console.write(fmt.Sprintf("model set to %s%s%s\n", cyan, name, reset))
}

// writeModels lists the available models, marking the active one.
func (c *chat) writeModels() {
	ids := models.availableIDs()
	c.mu.Lock()
	current := c.model
	c.mu.Unlock()
	c.console.write(fmt.Sprintf("%sModels (%d):%s\n", bold, len(ids), reset))
	for _, id := range ids {
		marker := " "
		if id == current {
			marker = "▶"
		}
		c.console.write(fmt.Sprintf("  %s %s\n", marker, id))
	}
}

// setModel records the active model and persists it for the next launch.
func (c *chat) setModel(id string) {
	c.mu.Lock()
	c.model = id
	c.mu.Unlock()
	c.state.Model = id
	c.persist()
}
