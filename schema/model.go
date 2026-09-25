package schema

// ModelID identifies a model.
type ModelID string

func (id ModelID) String() string {
	return string(id)
}

// ModelVariantID identifies a model variant.
type ModelVariantID string

// ModelFamily names a model family.
type ModelFamily string

// ModelRef references a model, its provider and an optional variant.
type ModelRef struct {
	ID         ModelID         `json:"id"`
	ProviderID ProviderID      `json:"providerID"`
	Variant    *ModelVariantID `json:"variant,omitempty"`
}

// ModelCapabilities lists what a model can do (tools, reasoning, ...).
type ModelCapabilities struct {
	Tools  bool     `json:"tools"`
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// ModelCostTier is one pricing tier.
type ModelCostTier struct {
	Type string `json:"type"`
	Size int    `json:"size"`
}

// ModelCostCache is the cache-read and cache-write pricing.
type ModelCostCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// ModelCost is a model's pricing.
type ModelCost struct {
	Tier   *ModelCostTier  `json:"tier,omitempty"`
	Input  float64         `json:"input"`
	Output float64         `json:"output"`
	Cache  *ModelCostCache `json:"cache,omitempty"`
}

// ProtocolID identifies a wire protocol (for example "openai-responses",
// "anthropic-messages" or "gemini"), selecting the route the SDK speaks.
type ProtocolID string

// ModelAPI identifies the wire protocol and an optional endpoint.
type ModelAPI struct {
	ID       *ModelID   `json:"id,omitempty"`
	Protocol ProtocolID `json:"protocol,omitempty"`
	URL      *string    `json:"url,omitempty"`
}

// ModelLimit is a model's token limits.
type ModelLimit struct {
	Context int  `json:"context"`
	Input   *int `json:"input,omitempty"`
	Output  int  `json:"output"`
}

// ModelTime is a model's release and knowledge-cutoff times.
type ModelTime struct {
	Released float64 `json:"released"`
}

// ModelVariant is one variant of a model.
type ModelVariant struct {
	ID      ModelVariantID    `json:"id"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
}

// ModelRequest is what a model sends with every request: the provider request
// options plus an optional default variant.
type ModelRequest struct {
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
	Variant *string           `json:"variant,omitempty"`
}

// ModelInfo is a model's catalog metadata: identity, limits, pricing, endpoint,
// headers and variants.
type ModelInfo struct {
	ID           ModelID           `json:"id"`
	ProviderID   ProviderID        `json:"providerID"`
	Family       *ModelFamily      `json:"family,omitempty"`
	Name         string            `json:"name"`
	API          ModelAPI          `json:"api"`
	Capabilities ModelCapabilities `json:"capabilities"`
	Request      ModelRequest      `json:"request"`
	Variants     []ModelVariant    `json:"variants"`
	Time         ModelTime         `json:"time"`
	Cost         []ModelCost       `json:"cost"`
	Status       string            `json:"status"`
	Enabled      bool              `json:"enabled"`
	Limit        ModelLimit        `json:"limit"`
}

// ModelInfoEmpty is an empty model info for a provider and model id.
func ModelInfoEmpty(providerID ProviderID, modelID ModelID) ModelInfo {
	id := modelID
	return ModelInfo{
		ID:         modelID,
		ProviderID: providerID,
		Name:       string(modelID),
		API:        ModelAPI{ID: &id},
		Capabilities: ModelCapabilities{
			Tools:  false,
			Input:  []string{},
			Output: []string{},
		},
		Request:  ModelRequest{Headers: map[string]string{}, Body: map[string]any{}},
		Variants: []ModelVariant{},
		Time:     ModelTime{Released: 0},
		Cost:     []ModelCost{},
		Status:   "active",
		Enabled:  true,
		Limit:    ModelLimit{Context: 0, Output: 0},
	}
}
