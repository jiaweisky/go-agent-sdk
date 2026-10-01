package main

import "github.com/user/go-agent-sdk/schema"

// deepseekPricing is the per-million-token peak price in CNY.
type deepseekPricing struct {
	cacheHit  float64
	cacheMiss float64
	output    float64
}

// Peak-hour rates from https://api-docs.deepseek.com/zh-cn/quick_start/pricing.
// Off-peak is half price.
var (
	deepseekFlashPricing = deepseekPricing{cacheHit: 0.04, cacheMiss: 2.0, output: 8.0}
	deepseekProPricing   = deepseekPricing{cacheHit: 0.30, cacheMiss: 9.0, output: 27.0}
)

// modelMetadata is the demo provider's per-model knowledge: the token limits the
// SDK uses to time automatic compaction, and the peak pricing the usage view
// applies. Only models this demo knows are listed; an unknown model gets no
// limit (automatic compaction stays off) and no price.
type modelMetadata struct {
	context int
	output  int
	pricing deepseekPricing
}

// modelCatalog maps a DeepSeek model id to its metadata. The context/output
// values are the provider's documented limits for the V4.1 family (1M context,
// 384K max output).
var modelCatalog = map[string]modelMetadata{
	"deepseek-v4-flash": {context: 1_000_000, output: 384_000, pricing: deepseekFlashPricing},
	"deepseek-v4-pro":   {context: 1_000_000, output: 384_000, pricing: deepseekProPricing},
}

// modelMetadataFor returns the metadata for modelID, if the demo knows it.
func modelMetadataFor(modelID string) (modelMetadata, bool) {
	meta, ok := modelCatalog[modelID]
	return meta, ok
}

// modelLimits returns modelID's token limits for the SDK's model info. An
// unknown model yields the zero limit, which disables automatic compaction.
func modelLimits(modelID string) schema.ModelLimit {
	meta, ok := modelMetadataFor(modelID)
	if !ok {
		return schema.ModelLimit{}
	}
	return schema.ModelLimit{Context: meta.context, Output: meta.output}
}
