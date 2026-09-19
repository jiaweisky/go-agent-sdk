package schema

import "encoding/json"

type ProviderOptions map[string]map[string]any

type HttpOptions struct {
	Body    JsonSchema        `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
}

func NewHttpOptions(body JsonSchema, headers, query map[string]string) *HttpOptions {
	return &HttpOptions{Body: body, Headers: headers, Query: query}
}

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

func NewGenerationOptions() *GenerationOptions {
	return &GenerationOptions{}
}

type ModelLimits struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

func NewModelLimits(context, output int) *ModelLimits {
	return &ModelLimits{Context: context, Output: output}
}

type ModelDefaults struct {
	Limits          *ModelLimits       `json:"limits,omitempty"`
	Generation      *GenerationOptions `json:"generation,omitempty"`
	ProviderOptions ProviderOptions    `json:"providerOptions,omitempty"`
	Http            *HttpOptions       `json:"http,omitempty"`
}

func NewModelDefaults(limits *ModelLimits, generation *GenerationOptions, providerOptions ProviderOptions, http *HttpOptions) *ModelDefaults {
	return &ModelDefaults{Limits: limits, Generation: generation, ProviderOptions: providerOptions, Http: http}
}

type ModelToolSchemaCompatibility string

const (
	ModelToolSchemaGemini   ModelToolSchemaCompatibility = "gemini"
	ModelToolSchemaMoonshot ModelToolSchemaCompatibility = "moonshot"
)

type ModelCompatibility struct {
	ToolSchema *ModelToolSchemaCompatibility `json:"toolSchema,omitempty"`
}

func NewModelCompatibility(toolSchema ModelToolSchemaCompatibility) *ModelCompatibility {
	return &ModelCompatibility{ToolSchema: &toolSchema}
}

type ModelSchema interface {
	ModelID() ModelID
	ProviderID() ProviderID
	GetDefaults() *ModelDefaults
	GetCompatibility() *ModelCompatibility
}

type Model struct {
	ID            ModelID             `json:"id"`
	Provider      ProviderID          `json:"provider"`
	Defaults      *ModelDefaults      `json:"defaults,omitempty"`
	Compatibility *ModelCompatibility `json:"compatibility,omitempty"`
}

func (m Model) ModelID() ModelID                      { return m.ID }
func (m Model) ProviderID() ProviderID                { return m.Provider }
func (m Model) GetDefaults() *ModelDefaults           { return m.Defaults }
func (m Model) GetCompatibility() *ModelCompatibility { return m.Compatibility }

func (m ModelID) ModelID() ModelID                      { return m }
func (m ModelID) ProviderID() ProviderID                { return "" }
func (m ModelID) GetDefaults() *ModelDefaults           { return nil }
func (m ModelID) GetCompatibility() *ModelCompatibility { return nil }

type ModelInput struct {
	ID            string
	Provider      string
	Defaults      *ModelDefaults
	Compatibility *ModelCompatibility
}

func NewModel(input ModelInput) Model {
	return Model{
		ID:            ModelID(input.ID),
		Provider:      ProviderID(input.Provider),
		Defaults:      input.Defaults,
		Compatibility: input.Compatibility,
	}
}

func ModelInputFrom(m Model) ModelInput {
	return ModelInput{
		ID:            string(m.ID),
		Provider:      string(m.Provider),
		Defaults:      m.Defaults,
		Compatibility: m.Compatibility,
	}
}

func ModelUpdate(m Model, patch ModelInput) Model {
	if patch.ID == "" && patch.Provider == "" && patch.Defaults == nil && patch.Compatibility == nil {
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
	return r
}

type CacheHint struct {
	Type       string `json:"type"` // "ephemeral" or "persistent"
	TTLSeconds *int   `json:"ttlSeconds,omitempty"`
}

func NewCacheHint(t string, ttlSeconds *int) CacheHint {
	return CacheHint{Type: t, TTLSeconds: ttlSeconds}
}

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

func CachePolicyAuto() *CachePolicy {
	return &CachePolicy{Auto: true}
}

func CachePolicyNone() *CachePolicy {
	return &CachePolicy{None: true}
}

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
			return defined[0]
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
		return defined[0]
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

func latestGeneration[T any](items []*GenerationOptions, getter func(*GenerationOptions) T) T {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i] != nil {
			return getter(items[i])
		}
	}
	var zero T
	return zero
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
