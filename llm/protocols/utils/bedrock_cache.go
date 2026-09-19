package utils

import "github.com/user/go-agent-sdk/llm/schema"

type CachePoint struct {
	Type string  `json:"type"`
	TTL  *string `json:"ttl,omitempty"`
}

type CachePointBlock struct {
	CachePoint CachePoint `json:"cachePoint"`
}

const BedrockBreakpointCap = 4

func NewBedrockBreakpoints() Breakpoints {
	return NewBreakpoints(BedrockBreakpointCap)
}

var cachePoint5M = CachePointBlock{CachePoint: CachePoint{Type: "default"}}

var cachePoint1H = CachePointBlock{CachePoint: CachePoint{Type: "default", TTL: strPtr("1h")}}

func strPtr(s string) *string { return &s }

func Block(breakpoints *Breakpoints, cache *schema.CacheHint) *CachePointBlock {
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
