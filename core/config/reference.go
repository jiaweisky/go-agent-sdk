package config

import (
	"bytes"
	"encoding/json"
)

// ReferenceGit mirrors ConfigReference.Git.
type ReferenceGit struct {
	Repository  string  `json:"repository"`
	Branch      *string `json:"branch,omitempty"`
	Description *string `json:"description,omitempty"`
	Hidden      *bool   `json:"hidden,omitempty"`
}

// ReferenceLocal mirrors ConfigReference.Local.
type ReferenceLocal struct {
	Path        string  `json:"path"`
	Description *string `json:"description,omitempty"`
	Hidden      *bool   `json:"hidden,omitempty"`
}

// ReferenceEntry mirrors ConfigReference.Entry (String | Git | Local).
type ReferenceEntry struct {
	Shorthand *string
	Git       *ReferenceGit
	Local     *ReferenceLocal
}

func (r *ReferenceEntry) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		r.Shorthand = &value
		r.Git = nil
		r.Local = nil
		return nil
	}
	var probe struct {
		Repository *string `json:"repository"`
	}
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return err
	}
	if probe.Repository != nil {
		var git ReferenceGit
		if err := json.Unmarshal(trimmed, &git); err != nil {
			return err
		}
		r.Git = &git
		r.Local = nil
		r.Shorthand = nil
		return nil
	}
	var local ReferenceLocal
	if err := json.Unmarshal(trimmed, &local); err != nil {
		return err
	}
	r.Local = &local
	r.Git = nil
	r.Shorthand = nil
	return nil
}

func (r ReferenceEntry) MarshalJSON() ([]byte, error) {
	switch {
	case r.Shorthand != nil:
		return json.Marshal(*r.Shorthand)
	case r.Git != nil:
		return json.Marshal(r.Git)
	default:
		return json.Marshal(r.Local)
	}
}

// ReferenceInfo mirrors ConfigReference.Info.
type ReferenceInfo map[string]ReferenceEntry
