package utils

import "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"

// CachePoint marks where the provider may cache in a Bedrock request.
type CachePoint struct {
	Type string  `json:"type"`
	TTL  *string `json:"ttl,omitempty"`
}

// CachePointBlock is a content block holding a cache point.
type CachePointBlock struct {
	CachePoint CachePoint `json:"cachePoint"`
}

// BedrockBreakpointCap is the number of cache breakpoints Bedrock allows.
const BedrockBreakpointCap = 4

// NewBedrockBreakpoints returns the breakpoint budget Bedrock allows.
func NewBedrockBreakpoints() Breakpoints {
	return NewBreakpoints(BedrockBreakpointCap)
}

var cachePoint5M = CachePointBlock{CachePoint: CachePoint{Type: "default"}}

var cachePoint1H = CachePointBlock{CachePoint: CachePoint{Type: "default", TTL: strPtr("1h")}}

func strPtr(s string) *string { return &s }

// BedrockBlock returns the cache-point block for a hint, spending one breakpoint,
// or nil
// when the hint is absent or the budget is spent.
func BedrockBlock(breakpoints *Breakpoints, cache *schema.CacheHint) *CachePointBlock {
	if cache == nil || (cache.Type != "ephemeral" && cache.Type != "persistent") {
		return nil
	}
	if breakpoints.Remaining <= 0 {
		breakpoints.Dropped++
		return nil
	}
	breakpoints.Remaining--
	ttl := TTLBucket(cache.TTLSeconds)
	if ttl != nil && *ttl == "1h" {
		return &cachePoint1H
	}
	return &cachePoint5M
}
