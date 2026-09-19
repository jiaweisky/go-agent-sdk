package config

import (
	"bytes"
	"encoding/json"
)

// V1LSPEntry mirrors ConfigLSPV1.Entry.
type V1LSPEntry struct {
	Command        []string          `json:"command,omitempty"`
	Extensions     []string          `json:"extensions,omitempty"`
	Disabled       *bool             `json:"disabled,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Initialization map[string]any    `json:"initialization,omitempty"`
}

// V1LSPInfo mirrors ConfigLSPV1.Info (Boolean | Record<string, Entry>).
type V1LSPInfo struct {
	Enabled *bool
	Servers map[string]V1LSPEntry
}

func (l *V1LSPInfo) UnmarshalJSON(data []byte) error {
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

func (l V1LSPInfo) MarshalJSON() ([]byte, error) {
	if l.Servers != nil {
		return json.Marshal(l.Servers)
	}
	if l.Enabled != nil {
		return json.Marshal(*l.Enabled)
	}
	return []byte("false"), nil
}

// V1BuiltinServerIDs keeps this list aligned with the builtin servers in
// opencode's LSP runtime.
var V1BuiltinServerIDs = []string{
	"deno",
	"typescript",
	"vue",
	"eslint",
	"oxlint",
	"biome",
	"gopls",
	"ruby-lsp",
	"ty",
	"pyright",
	"elixir-ls",
	"zls",
	"csharp",
	"razor",
	"fsharp",
	"sourcekit-lsp",
	"rust",
	"clangd",
	"svelte",
	"astro",
	"jdtls",
	"kotlin-ls",
	"yaml-ls",
	"lua-ls",
	"php intelephense",
	"prisma",
	"dart",
	"ocaml-lsp",
	"bash",
	"terraform",
	"texlab",
	"dockerfile",
	"gleam",
	"clojure-lsp",
	"nixd",
	"tinymist",
	"haskell-language-server",
	"julials",
}
