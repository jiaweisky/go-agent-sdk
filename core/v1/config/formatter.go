package config

import (
	"bytes"
	"encoding/json"
)

// V1FormatterEntry mirrors ConfigFormatterV1.Entry.
type V1FormatterEntry struct {
	Disabled    *bool             `json:"disabled,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Extensions  []string          `json:"extensions,omitempty"`
}

// V1FormatterInfo mirrors ConfigFormatterV1.Info (Boolean | Record<string, Entry>).
type V1FormatterInfo struct {
	Enabled *bool
	Entries map[string]V1FormatterEntry
}

func (f *V1FormatterInfo) UnmarshalJSON(data []byte) error {
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

func (f V1FormatterInfo) MarshalJSON() ([]byte, error) {
	if f.Entries != nil {
		return json.Marshal(f.Entries)
	}
	if f.Enabled != nil {
		return json.Marshal(*f.Enabled)
	}
	return []byte("false"), nil
}
