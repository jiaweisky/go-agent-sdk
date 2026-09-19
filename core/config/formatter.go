package config

import (
	"bytes"
	"encoding/json"
)

// FormatterEntry mirrors ConfigFormatter.Entry.
type FormatterEntry struct {
	Disabled    *bool             `json:"disabled,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Extensions  []string          `json:"extensions,omitempty"`
}

// FormatterInfo mirrors ConfigFormatter.Info (Boolean | Record<string, Entry>).
type FormatterInfo struct {
	Enabled *bool
	Entries map[string]FormatterEntry
}

func (f *FormatterInfo) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == 't' || trimmed[0] == 'f') {
		var value bool
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		f.Enabled = &value
		f.Entries = nil
		return nil
	}
	f.Enabled = nil
	return json.Unmarshal(trimmed, &f.Entries)
}

func (f FormatterInfo) MarshalJSON() ([]byte, error) {
	if f.Entries != nil {
		return json.Marshal(f.Entries)
	}
	if f.Enabled != nil {
		return json.Marshal(*f.Enabled)
	}
	return []byte("false"), nil
}
