// Package modelinfo adapts a config model entry to the SDK's model metadata.
package modelinfo

import (
	"fmt"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// Config is the config data needed to build one model's metadata.
type Config struct {
	ID           string
	Protocol     string
	Model        string
	BaseURL      string
	ProviderID   schema.ProviderID
	ContextLimit int
	OutputLimit  int
	Inputs       []string
	Forms        map[string][]string
	MIMEs        []string
	Headers      map[string]string
}

// New builds the SDK model metadata for a config entry.
func New(c Config) (schema.ModelInfo, error) {
	info, err := agent.NewModelInfo(
		schema.ProtocolID(c.Protocol),
		c.Model,
		c.BaseURL,
		schema.ModelLimit{Context: c.ContextLimit, Output: c.OutputLimit},
	)
	if err != nil {
		return schema.ModelInfo{}, err
	}
	info.ID = schema.ModelID(c.ID)
	info.ProviderID = c.ProviderID
	if len(c.Inputs) > 0 {
		info.Capabilities.Input = c.Inputs
	}
	if len(c.Forms) > 0 || len(c.MIMEs) > 0 {
		info.Capabilities.Media = &schema.ModelMediaCapability{Forms: c.Forms, MIMEs: c.MIMEs}
	}
	for name, value := range c.Headers {
		info.Request.Headers[name] = value
	}
	return info, nil
}

// FromConfig builds a model entry's SDK metadata.
func FromConfig(entry config.ModelConfig) (schema.ModelInfo, error) {
	var forms map[string][]string
	if entry.Media != nil {
		forms = entry.Media.Forms
	}
	return New(Config{
		ID:           entry.ID,
		Protocol:     entry.Protocol,
		Model:        entry.Model,
		BaseURL:      entry.Endpoint(),
		ProviderID:   entry.ProviderKey(),
		ContextLimit: entry.ContextLimit,
		OutputLimit:  entry.OutputLimit,
		Inputs:       entry.EffectiveInputs(),
		Forms:        forms,
		MIMEs:        entry.InputMIMEs,
		Headers:      entry.Headers,
	})
}

// Resolve returns a model id's SDK metadata. An unknown id falls back to the
// registry's default entry.
func Resolve(registry config.ModelRegistry, id string) (schema.ModelInfo, error) {
	entry, ok := registry.Entry(id)
	if !ok {
		entry, ok = registry.DefaultEntry()
	}
	if !ok {
		return schema.ModelInfo{}, fmt.Errorf("model %q: model registry has no entry", id)
	}
	return FromConfig(entry)
}

// List resolves every registry model's metadata in registration order.
func List(registry config.ModelRegistry) ([]schema.ModelInfo, error) {
	ids := registry.IDs()
	if len(ids) == 0 {
		return nil, fmt.Errorf("model registry has no models")
	}
	infos := make([]schema.ModelInfo, 0, len(ids))
	for _, id := range ids {
		info, err := Resolve(registry, id)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}
