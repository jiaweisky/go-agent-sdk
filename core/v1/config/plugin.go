package config

import (
	"bytes"
	"encoding/json"
)

// V1PluginOptions mirrors ConfigPluginV1.Options.
type V1PluginOptions map[string]any

// V1PluginSpec mirrors ConfigPluginV1.Spec (String | [String, Options]).
type V1PluginSpec struct {
	Name    *string
	Package *string
	Options V1PluginOptions
}

func (p *V1PluginSpec) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		p.Name = &value
		return nil
	}
	var tuple []json.RawMessage
	if err := json.Unmarshal(trimmed, &tuple); err != nil {
		return err
	}
	if len(tuple) > 0 {
		var value string
		if err := json.Unmarshal(tuple[0], &value); err != nil {
			return err
		}
		p.Package = &value
	}
	if len(tuple) > 1 {
		_ = json.Unmarshal(tuple[1], &p.Options)
	}
	return nil
}

func (p V1PluginSpec) MarshalJSON() ([]byte, error) {
	if p.Name != nil {
		return json.Marshal(*p.Name)
	}
	return json.Marshal([]any{p.Package, p.Options})
}
