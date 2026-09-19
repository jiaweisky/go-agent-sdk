package config

import (
	"bytes"
	"encoding/json"
)

// PluginEntry mirrors ConfigPlugin.Entry.
type PluginEntry struct {
	Package string         `json:"package"`
	Options map[string]any `json:"options,omitempty"`
}

// Plugin mirrors ConfigPlugin.Plugin (String | Entry).
type Plugin struct {
	Name  *string
	Entry *PluginEntry
}

func (p *Plugin) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		p.Name = &value
		p.Entry = nil
		return nil
	}
	var entry PluginEntry
	if err := json.Unmarshal(trimmed, &entry); err != nil {
		return err
	}
	p.Entry = &entry
	p.Name = nil
	return nil
}

func (p Plugin) MarshalJSON() ([]byte, error) {
	if p.Name != nil {
		return json.Marshal(*p.Name)
	}
	return json.Marshal(p.Entry)
}

// Plugins mirrors ConfigPlugin.Plugins.
type Plugins []Plugin
