package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// chatState is the example's persisted selection, so the next launch reuses the
// last model and agent the user chose.
type chatState struct {
	Model string `json:"model,omitempty"`
	Agent string `json:"agent,omitempty"`
}

// loadState reads the chat state, returning a zero value when it is missing or
// malformed.
func loadState(path string) chatState {
	var state chatState
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	return state
}

// saveState writes the chat state, best-effort.
func saveState(path string, state chatState) {
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// persist saves the chat's current selection.
func (c *chat) persist() {
	saveState(c.statePath, c.state)
}
