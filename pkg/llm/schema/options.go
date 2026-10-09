package schema

import "encoding/json"

// ProviderOptions carries provider-specific request options, keyed by provider.
type ProviderOptions map[string]map[string]any

// HttpOptions adds body, header and query values to a request.
type HttpOptions struct {
	Body    JsonSchema        `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
}

// NewHttpOptions builds http options.
func NewHttpOptions(body JsonSchema, headers, query map[string]string) *HttpOptions {
	return &HttpOptions{Body: body, Headers: headers, Query: query}
}

// GenerationOptions controls sampling: token limits, temperature, penalties, and
// the stop sequences.
type GenerationOptions struct {
	MaxTokens        *int     `json:"maxTokens,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"topP,omitempty"`
	TopK             *float64 `json:"topK,omitempty"`
	FrequencyPenalty *float64 `json:"frequencyPenalty,omitempty"`
	PresencePenalty  *float64 `json:"presencePenalty,omitempty"`
	Seed             *int     `json:"seed,omitempty"`
	Stop             []string `json:"stop,omitempty"`
}

// ModelLimits is a model's context and output token limits.
type ModelLimits struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

// NewModelLimits builds model limits.
func NewModelLimits(context, output int) *ModelLimits {
	return &ModelLimits{Context: context, Output: output}
}

// ModelDefaults is what a model applies to a request the caller left unset.
type ModelDefaults struct {
	Limits          *ModelLimits       `json:"limits,omitempty"`
	Generation      *GenerationOptions `json:"generation,omitempty"`
	ProviderOptions ProviderOptions    `json:"providerOptions,omitempty"`
	Http            *HttpOptions       `json:"http,omitempty"`
}

// ModelToolSchemaCompatibility names a tool-schema shape a model requires.
type ModelToolSchemaCompatibility string

const (
	ModelToolSchemaGemini   ModelToolSchemaCompatibility = "gemini"
	ModelToolSchemaMoonshot ModelToolSchemaCompatibility = "moonshot"
)

// ModelCapabilities lists a model's input and output modalities (for example
// "text", "image", "audio", "video", "pdf"), mirroring the model catalog's
// modalities. An empty Input means the model's media support is not declared,
// so only the protocol's wire support constrains it.
type ModelCapabilities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
	// Sources, when set, are the media sources the model accepts (inline, url,
	// file); empty means the protocol decides.
	Sources []MediaSource `json:"sources,omitempty"`
}

// ModelCompatibility records the schema shapes a model requires.
type ModelCompatibility struct {
	ToolSchema *ModelToolSchemaCompatibility `json:"toolSchema,omitempty"`
}

// NewModelCompatibility builds compatibility for one tool-schema shape.
func NewModelCompatibility(toolSchema ModelToolSchemaCompatibility) *ModelCompatibility {
	return &ModelCompatibility{ToolSchema: &toolSchema}
}

// ModelSchema is the model surface a request needs: its identity, its defaults,
// and the schema shapes it requires.
type ModelSchema interface {
	ModelID() ModelID
	ProviderID() ProviderID
	GetDefaults() *ModelDefaults
	// GetRouteDefaults returns the defaults of the model's route, or nil when it
	// has none.
	GetRouteDefaults() *ModelDefaults
	GetCompatibility() *ModelCompatibility
	// GetCapabilities returns the model's input/output modalities, or nil when
	// they are not declared.
	GetCapabilities() *ModelCapabilities
}

// Model is a concrete model: its identity, defaults, compatibility and
// capabilities. It implements ModelSchema.
type Model struct {
	ID            ModelID             `json:"id"`
	Provider      ProviderID          `json:"provider"`
	Defaults      *ModelDefaults      `json:"defaults,omitempty"`
	Compatibility *ModelCompatibility `json:"compatibility,omitempty"`
	Capabilities  *ModelCapabilities  `json:"capabilities,omitempty"`
}

// ModelID returns the model's id.
func (m Model) ModelID() ModelID { return m.ID }

// ProviderID returns the model's provider.
func (m Model) ProviderID() ProviderID { return m.Provider }

// GetDefaults returns the model's defaults.
func (m Model) GetDefaults() *ModelDefaults { return m.Defaults }

// GetRouteDefaults returns nil: a bare model carries no route.
func (m Model) GetRouteDefaults() *ModelDefaults { return nil }

// GetCompatibility returns the model's compatibility.
func (m Model) GetCompatibility() *ModelCompatibility { return m.Compatibility }

// GetCapabilities returns the model's input/output modalities.
func (m Model) GetCapabilities() *ModelCapabilities { return m.Capabilities }

// ModelID implements ModelSchema as a bare model id: only the id is known.
func (m ModelID) ModelID() ModelID { return m }

// ProviderID returns "": a bare id has no provider.
func (m ModelID) ProviderID() ProviderID { return "" }

// GetDefaults returns nil: a bare id carries no defaults.
func (m ModelID) GetDefaults() *ModelDefaults { return nil }

// GetRouteDefaults returns nil: a bare id carries no route.
func (m ModelID) GetRouteDefaults() *ModelDefaults { return nil }

// GetCompatibility returns nil: a bare id carries no compatibility.
func (m ModelID) GetCompatibility() *ModelCompatibility { return nil }

// GetCapabilities returns nil: a bare id carries no capabilities.
func (m ModelID) GetCapabilities() *ModelCapabilities { return nil }

// ModelInput is the input form of a Model.
type ModelInput struct {
	ID            string
	Provider      string
	Defaults      *ModelDefaults
	Compatibility *ModelCompatibility
	Capabilities  *ModelCapabilities
}

// NewModel builds a model from input.
func NewModel(input ModelInput) Model {
	return Model{
		ID:            ModelID(input.ID),
		Provider:      ProviderID(input.Provider),
		Defaults:      input.Defaults,
		Compatibility: input.Compatibility,
		Capabilities:  input.Capabilities,
	}
}

// ModelUpdate applies the set fields of patch and returns the updated model.
func ModelUpdate(m Model, patch ModelInput) Model {
	if patch.ID == "" && patch.Provider == "" && patch.Defaults == nil &&
		patch.Compatibility == nil && patch.Capabilities == nil {
		return m
	}
	r := m
	if patch.ID != "" {
		r.ID = ModelID(patch.ID)
	}
	if patch.Provider != "" {
		r.Provider = ProviderID(patch.Provider)
	}
	if patch.Defaults != nil {
		r.Defaults = patch.Defaults
	}
	if patch.Compatibility != nil {
		r.Compatibility = patch.Compatibility
	}
	if patch.Capabilities != nil {
		r.Capabilities = patch.Capabilities
	}
	return r
}

// CacheHint marks content the provider may cache.
type CacheHint struct {
	Type       string `json:"type"` // "ephemeral" or "persistent"
	TTLSeconds *int   `json:"ttlSeconds,omitempty"`
}

// NewCacheHint builds a cache hint.
func NewCacheHint(t string, ttlSeconds *int) CacheHint {
	return CacheHint{Type: t, TTLSeconds: ttlSeconds}
}

// CachePolicyMessages selects a number of trailing messages to cache.
type CachePolicyMessages struct {
	Tail *int `json:"tail,omitempty"`
}

// CachePolicy.Messages accepts:
//   - "latest-user-message"
//   - "latest-assistant"
//   - CachePolicyMessages{Tail: &n}
type CachePolicy struct {
	Auto       bool  `json:"-"`
	None       bool  `json:"-"`
	Tools      *bool `json:"tools,omitempty"`
	System     *bool `json:"system,omitempty"`
	Messages   any   `json:"messages,omitempty"`
	TTLSeconds *int  `json:"ttlSeconds,omitempty"`
}

// CachePolicyAuto caches tools, the system prompt and the latest messages.
func CachePolicyAuto() *CachePolicy {
	return &CachePolicy{Auto: true}
}

// CachePolicyNone disables caching.
func CachePolicyNone() *CachePolicy {
	return &CachePolicy{None: true}
}

// MarshalJSON renders the policy as "auto", "none", or an object of flags.
func (c *CachePolicy) MarshalJSON() ([]byte, error) {
	if c.Auto {
		return json.Marshal("auto")
	}
	if c.None {
		return json.Marshal("none")
	}
	obj := make(map[string]any)
	if c.Tools != nil {
		obj["tools"] = *c.Tools
	}
	if c.System != nil {
		obj["system"] = *c.System
	}
	if c.Messages != nil {
		obj["messages"] = c.Messages
	}
	if c.TTLSeconds != nil {
		obj["ttlSeconds"] = *c.TTLSeconds
	}
	if len(obj) == 0 {
		return json.Marshal("none")
	}
	return json.Marshal(obj)
}

// UnmarshalJSON accepts "auto", "none", or an object of flags.
func (c *CachePolicy) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		switch s {
		case "auto":
			c.Auto = true
		case "none":
			c.None = true
		}
		return nil
	}
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	for k, v := range obj {
		switch k {
		case "tools":
			var b bool
			if err := json.Unmarshal(v, &b); err == nil {
				c.Tools = &b
			}
		case "system":
			var b bool
			if err := json.Unmarshal(v, &b); err == nil {
				c.System = &b
			}
		case "messages":
			c.Messages = v
		case "ttlSeconds":
			var n int
			if err := json.Unmarshal(v, &n); err == nil {
				c.TTLSeconds = &n
			}
		}
	}
	return nil
}

// MergeJsonRecords merges records left to right, later keys winning.
func MergeJsonRecords(items ...map[string]any) map[string]any {
	defined := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item != nil {
			defined = append(defined, item)
		}
	}
	if len(defined) == 0 {
		return nil
	}
	if len(defined) == 1 {
		allSet := true
		for _, v := range defined[0] {
			if v == nil {
				allSet = false
				break
			}
		}
		if allSet {
			result := make(map[string]any, len(defined[0]))
			for k, v := range defined[0] {
				result[k] = v
			}
			return result
		}
	}
	result := make(map[string]any)
	for _, item := range defined {
		for k, v := range item {
			if v == nil {
				continue
			}
			existing, hasExisting := result[k]
			existingMap, existingIsMap := existing.(map[string]any)
			vMap, vIsMap := v.(map[string]any)
			if hasExisting && existingIsMap && vIsMap {
				result[k] = MergeJsonRecords(existingMap, vMap)
			} else {
				result[k] = v
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// MergeStringRecords merges string records left to right, later keys winning.
func MergeStringRecords(items ...map[string]string) map[string]string {
	defined := make([]map[string]string, 0, len(items))
	for _, item := range items {
		if item != nil {
			defined = append(defined, item)
		}
	}
	if len(defined) == 0 {
		return nil
	}
	if len(defined) == 1 {
		result := make(map[string]string, len(defined[0]))
		for k, v := range defined[0] {
			result[k] = v
		}
		return result
	}
	result := make(map[string]string)
	for _, item := range defined {
		for k, v := range item {
			result[k] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// MergeProviderOptions merges provider options left to right, later providers and
// keys winning.
func MergeProviderOptions(items ...ProviderOptions) ProviderOptions {
	result := make(map[string]map[string]any)
	for _, item := range items {
		if item == nil {
			continue
		}
		for provider, opts := range item {
			merged := MergeJsonRecords(result[provider], opts)
			if merged != nil {
				result[provider] = merged
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// MergeHttpOptions merges http options: bodies and headers are merged, query
// values are taken from the last one that sets them.
func MergeHttpOptions(items ...*HttpOptions) *HttpOptions {
	bodies := make([]map[string]any, 0, len(items))
	headers := make([]map[string]string, 0, len(items))
	queries := make([]map[string]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		bodies = append(bodies, item.Body)
		headers = append(headers, item.Headers)
		queries = append(queries, item.Query)
	}
	body := MergeJsonRecords(bodies...)
	header := MergeStringRecords(headers...)
	query := MergeStringRecords(queries...)
	if body == nil && header == nil && query == nil {
		return nil
	}
	return NewHttpOptions(body, header, query)
}

func latestGenerationPtr[T any](items []*GenerationOptions, getter func(*GenerationOptions) *T) *T {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i] != nil {
			if v := getter(items[i]); v != nil {
				return v
			}
		}
	}
	return nil
}

func latestStop(items []*GenerationOptions) []string {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i] != nil && items[i].Stop != nil {
			return items[i].Stop
		}
	}
	return nil
}

// MergeGenerationOptions merges generation options left to right: a later
// non-zero value wins.
func MergeGenerationOptions(items ...*GenerationOptions) *GenerationOptions {
	maxTokens := latestGenerationPtr(items, func(o *GenerationOptions) *int { return o.MaxTokens })
	temperature := latestGenerationPtr(items, func(o *GenerationOptions) *float64 { return o.Temperature })
	topP := latestGenerationPtr(items, func(o *GenerationOptions) *float64 { return o.TopP })
	topK := latestGenerationPtr(items, func(o *GenerationOptions) *float64 { return o.TopK })
	freqPenalty := latestGenerationPtr(items, func(o *GenerationOptions) *float64 { return o.FrequencyPenalty })
	presPenalty := latestGenerationPtr(items, func(o *GenerationOptions) *float64 { return o.PresencePenalty })
	seed := latestGenerationPtr(items, func(o *GenerationOptions) *int { return o.Seed })
	stop := latestStop(items)

	if maxTokens == nil && temperature == nil && topP == nil && topK == nil &&
		freqPenalty == nil && presPenalty == nil && seed == nil && stop == nil {
		return nil
	}
	return &GenerationOptions{
		MaxTokens: maxTokens, Temperature: temperature, TopP: topP, TopK: topK,
		FrequencyPenalty: freqPenalty, PresencePenalty: presPenalty, Seed: seed, Stop: stop,
	}
}
