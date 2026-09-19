package config

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/schema"
)

// Request mirrors ConfigProvider.Request.
type Request struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
}

// Cache mirrors ConfigProvider.Model cost cache.
type Cache struct {
	Read  *float64 `json:"read,omitempty"`
	Write *float64 `json:"write,omitempty"`
}

// CostTier mirrors the context pricing tier.
type CostTier struct {
	Type string `json:"type"`
	Size int    `json:"size"`
}

// Cost mirrors ConfigProvider.Model cost.
type Cost struct {
	Tier   *CostTier `json:"tier,omitempty"`
	Input  float64   `json:"input"`
	Output float64   `json:"output"`
	Cache  *Cache    `json:"cache,omitempty"`
}

// CostList accepts either a single Cost or an array of costs.
type CostList []Cost

func (l *CostList) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '[' {
		return json.Unmarshal(data, (*[]Cost)(l))
	}
	var one Cost
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*l = []Cost{one}
	return nil
}

// Limit mirrors ConfigProvider.Model limit.
type Limit struct {
	Context *int `json:"context,omitempty"`
	Input   *int `json:"input,omitempty"`
	Output  *int `json:"output,omitempty"`
}

// ModelAPI mirrors the ConfigProvider ModelApi union: an optional id plus the
// AISDK/Native provider fields.
type ModelAPI struct {
	ID       *schema.ModelID `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Package  string          `json:"package,omitempty"`
	URL      *string         `json:"url,omitempty"`
	Settings map[string]any  `json:"settings,omitempty"`
}

// ModelRequest mirrors ConfigProvider.Model request.
type ModelRequest struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
	Variant *string           `json:"variant,omitempty"`
}

// Variant mirrors ConfigProvider.Model variants entry.
type Variant struct {
	ID      schema.ModelVariantID `json:"id"`
	Headers map[string]string     `json:"headers,omitempty"`
	Body    map[string]any        `json:"body,omitempty"`
}

// Model mirrors ConfigProvider.Model.
type Model struct {
	Family       *schema.ModelFamily       `json:"family,omitempty"`
	Name         *string                   `json:"name,omitempty"`
	API          *ModelAPI                 `json:"api,omitempty"`
	Capabilities *schema.ModelCapabilities `json:"capabilities,omitempty"`
	Request      *ModelRequest             `json:"request,omitempty"`
	Variants     []Variant                 `json:"variants,omitempty"`
	Cost         CostList                  `json:"cost,omitempty"`
	Disabled     *bool                     `json:"disabled,omitempty"`
	Limit        *Limit                    `json:"limit,omitempty"`
}

// Info mirrors ConfigProvider.Info.
type Info struct {
	Name    *string             `json:"name,omitempty"`
	Env     []string            `json:"env,omitempty"`
	API     *schema.ProviderAPI `json:"api,omitempty"`
	Request *Request            `json:"request,omitempty"`
	Models  map[string]Model    `json:"models,omitempty"`
}
