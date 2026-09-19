package config

import (
	"bytes"
	"encoding/json"
)

// LSPEntry mirrors ConfigLSP.Entry (Disabled | Server). Disabled entries carry
// only `disabled: true`; server entries carry a command.
type LSPEntry struct {
	Command        []string          `json:"command,omitempty"`
	Extensions     []string          `json:"extensions,omitempty"`
	Disabled       *bool             `json:"disabled,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Initialization map[string]any    `json:"initialization,omitempty"`
}

// LSPInfo mirrors ConfigLSP.Info (Boolean | Record<string, Entry>).
type LSPInfo struct {
	Enabled *bool
	Servers map[string]LSPEntry
}

func (l *LSPInfo) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == 't' || trimmed[0] == 'f') {
		var value bool
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		l.Enabled = &value
		l.Servers = nil
		return nil
	}
	l.Enabled = nil
	return json.Unmarshal(trimmed, &l.Servers)
}

func (l LSPInfo) MarshalJSON() ([]byte, error) {
	if l.Servers != nil {
		return json.Marshal(l.Servers)
	}
	if l.Enabled != nil {
		return json.Marshal(*l.Enabled)
	}
	return []byte("false"), nil
}
