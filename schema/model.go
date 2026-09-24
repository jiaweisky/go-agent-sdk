package schema

// ModelID mirrors the branded ModelV2.ID string.
type ModelID string

func (id ModelID) String() string {
	return string(id)
}

// ModelVariantID mirrors the branded VariantID string.
type ModelVariantID string

// ModelFamily mirrors the branded Family string.
type ModelFamily string

// ModelRef mirrors Model.Ref.
type ModelRef struct {
	ID         ModelID         `json:"id"`
	ProviderID ProviderID      `json:"providerID"`
	Variant    *ModelVariantID `json:"variant,omitempty"`
}

// ModelCapabilities mirrors Model.Capabilities.
type ModelCapabilities struct {
	Tools  bool     `json:"tools"`
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// ModelCostTier mirrors the Model.Cost tier struct.
type ModelCostTier struct {
	Type string `json:"type"`
	Size int    `json:"size"`
}

// ModelCostCache mirrors the Model.Cost cache struct.
type ModelCostCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// ModelCost mirrors Model.Cost.
type ModelCost struct {
	Tier   *ModelCostTier  `json:"tier,omitempty"`
	Input  float64         `json:"input"`
	Output float64         `json:"output"`
	Cache  *ModelCostCache `json:"cache,omitempty"`
}

// ProtocolID identifies a native wire protocol (for example "openai-responses",
// "anthropic-messages" or "gemini"). It selects the route the SDK speaks. It
// mirrors llm/schema.ProtocolID; values convert trivially.
type ProtocolID string

// ModelAPI mirrors Model.Api: the native protocol plus an optional endpoint.
type ModelAPI struct {
	ID       *ModelID   `json:"id,omitempty"`
	Type     string     `json:"type"`
	Protocol ProtocolID `json:"protocol,omitempty"`
	URL      *string    `json:"url,omitempty"`
}

// ModelLimit mirrors the Model.Info limit struct.
type ModelLimit struct {
	Context int  `json:"context"`
	Input   *int `json:"input,omitempty"`
	Output  int  `json:"output"`
}

// ModelTime mirrors the Model.Info time struct.
type ModelTime struct {
	Released float64 `json:"released"`
}

// ModelVariant mirrors the Model.Info variants entry.
type ModelVariant struct {
	ID      ModelVariantID    `json:"id"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    map[string]any    `json:"body,omitempty"`
}

// ModelRequest mirrors Model.Info.request: Provider.Request plus an optional
// default variant.
type ModelRequest struct {
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
	Variant *string           `json:"variant,omitempty"`
}

// ModelInfo mirrors ModelV2.Info.
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

// ModelInfoEmpty mirrors Model.Info.empty.
func ModelInfoEmpty(providerID ProviderID, modelID ModelID) ModelInfo {
	id := modelID
	return ModelInfo{
		ID:         modelID,
		ProviderID: providerID,
		Name:       string(modelID),
		API:        ModelAPI{ID: &id, Type: "native"},
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
