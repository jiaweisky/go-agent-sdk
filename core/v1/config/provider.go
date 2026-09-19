package config

import "encoding/json"

// V1ProviderModelCost mirrors ConfigProviderV1.Model cost.
type V1ProviderModelCost struct {
	Input           float64                      `json:"input"`
	Output          float64                      `json:"output"`
	CacheRead       *float64                     `json:"cache_read,omitempty"`
	CacheWrite      *float64                     `json:"cache_write,omitempty"`
	ContextOver200k *V1ProviderModelCostOver200k `json:"context_over_200k,omitempty"`
}

// V1ProviderModelCostOver200k mirrors the nested context_over_200k cost.
type V1ProviderModelCostOver200k struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

// V1ProviderModelLimit mirrors ConfigProviderV1.Model limit.
type V1ProviderModelLimit struct {
	Context float64  `json:"context"`
	Input   *float64 `json:"input,omitempty"`
	Output  float64  `json:"output"`
}

// V1ProviderModelModalities mirrors ConfigProviderV1.Model modalities.
type V1ProviderModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// V1ProviderModelProvider mirrors ConfigProviderV1.Model provider.
type V1ProviderModelProvider struct {
	NPM *string `json:"npm,omitempty"`
	API *string `json:"api,omitempty"`
}

// V1ProviderModel mirrors ConfigProviderV1.Model.
type V1ProviderModel struct {
	ID           *string                    `json:"id,omitempty"`
	Name         *string                    `json:"name,omitempty"`
	Family       *string                    `json:"family,omitempty"`
	ReleaseDate  *string                    `json:"release_date,omitempty"`
	Attachment   *bool                      `json:"attachment,omitempty"`
	Reasoning    *bool                      `json:"reasoning,omitempty"`
	Temperature  *bool                      `json:"temperature,omitempty"`
	ToolCall     *bool                      `json:"tool_call,omitempty"`
	Interleaved  json.RawMessage            `json:"interleaved,omitempty"`
	Cost         *V1ProviderModelCost       `json:"cost,omitempty"`
	Limit        *V1ProviderModelLimit      `json:"limit,omitempty"`
	Modalities   *V1ProviderModelModalities `json:"modalities,omitempty"`
	Experimental *bool                      `json:"experimental,omitempty"`
	Status       *string                    `json:"status,omitempty"`
	Provider     *V1ProviderModelProvider   `json:"provider,omitempty"`
	Options      map[string]any             `json:"options,omitempty"`
	Headers      map[string]string          `json:"headers,omitempty"`
	Variants     map[string]json.RawMessage `json:"variants,omitempty"`
}

// V1ProviderInfo mirrors ConfigProviderV1.Info.
type V1ProviderInfo struct {
	API       *string                    `json:"api,omitempty"`
	Name      *string                    `json:"name,omitempty"`
	Env       []string                   `json:"env,omitempty"`
	ID        *string                    `json:"id,omitempty"`
	NPM       *string                    `json:"npm,omitempty"`
	Whitelist []string                   `json:"whitelist,omitempty"`
	Blacklist []string                   `json:"blacklist,omitempty"`
	Options   map[string]any             `json:"options,omitempty"`
	Models    map[string]V1ProviderModel `json:"models,omitempty"`
}
