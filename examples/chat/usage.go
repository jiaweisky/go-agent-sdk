package main

import (
	"fmt"
	"strings"
	"time"
)

// chatUsage accumulates provider usage and its cost. The SDK does not do this:
// it only exposes the raw usage (agent.Usage.ProviderMetadata), and the app
// interprets the provider's buckets and prices them.
type chatUsage struct {
	cacheHit  int
	cacheMiss int
	output    int
	cost      float64 // CNY
}

func (u chatUsage) tokens() int { return u.cacheHit + u.cacheMiss + u.output }

// addUsage folds one turn's raw provider usage into the totals, pricing it with
// the DeepSeek schedule (the demo provider).
func (u *chatUsage) addUsage(modelID string, providerMetadata map[string]map[string]any, at time.Time) {
	raw := providerMetadata["openai"] // openai-chat protocol key (DeepSeek speaks it)
	if raw == nil {
		for _, value := range providerMetadata {
			raw = value
			break
		}
	}
	if raw == nil {
		return
	}
	hit := numberField(raw, "prompt_cache_hit_tokens")
	miss := numberField(raw, "prompt_cache_miss_tokens")
	if hit == 0 && miss == 0 {
		// Fall back to the generic OpenAI-compatible shape.
		hit = nestedNumber(raw, "prompt_tokens_details", "cached_tokens")
		miss = numberField(raw, "prompt_tokens") - hit
	}
	output := numberField(raw, "completion_tokens")

	u.cacheHit += int(hit)
	u.cacheMiss += int(miss)
	u.output += int(output)
	if cost, ok := deepseekCost(modelID, hit, miss, output, at); ok {
		u.cost += cost
	}
}

// deepseekPricing is the per-million-token peak price in CNY.
type deepseekPricing struct {
	cacheHit  float64
	cacheMiss float64
	output    float64
}

// deepseekPeakPricing mirrors https://api-docs.deepseek.com/zh-cn/quick_start/pricing
// (peak hours). Off-peak is half price.
var deepseekPeakPricing = map[string]deepseekPricing{
	"flash": {cacheHit: 0.04, cacheMiss: 2.0, output: 8.0},
	"pro":   {cacheHit: 0.30, cacheMiss: 9.0, output: 27.0},
}

func deepseekPricingFor(modelID string) (deepseekPricing, bool) {
	switch {
	case strings.Contains(modelID, "pro"):
		return deepseekPeakPricing["pro"], true
	case strings.Contains(modelID, "flash"), strings.Contains(modelID, "deepseek"):
		return deepseekPeakPricing["flash"], true
	default:
		return deepseekPricing{}, false
	}
}

// deepseekCost prices one turn. It returns ok=false for models it does not know.
func deepseekCost(modelID string, cacheHit, cacheMiss, output float64, at time.Time) (float64, bool) {
	pricing, ok := deepseekPricingFor(modelID)
	if !ok {
		return 0, false
	}
	if !deepseekPeak(at) {
		pricing.cacheHit /= 2
		pricing.cacheMiss /= 2
		pricing.output /= 2
	}
	cost := (cacheHit*pricing.cacheHit + cacheMiss*pricing.cacheMiss + output*pricing.output) / 1_000_000
	return cost, true
}

// deepseekPeak reports whether at falls in DeepSeek's peak window: Beijing time,
// Monday-Friday, 09:00-12:00 or 14:00-18:00 (Chinese public holidays ignored).
func deepseekPeak(at time.Time) bool {
	beijing := at.In(time.FixedZone("UTC+8", 8*60*60))
	if day := beijing.Weekday(); day == time.Saturday || day == time.Sunday {
		return false
	}
	hour := float64(beijing.Hour()) + float64(beijing.Minute())/60
	return (hour >= 9 && hour < 12) || (hour >= 14 && hour < 18)
}

func numberField(data map[string]any, key string) float64 {
	switch value := data[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func nestedNumber(data map[string]any, keys ...string) float64 {
	for i, key := range keys {
		if i == len(keys)-1 {
			return numberField(data, key)
		}
		next, ok := data[key].(map[string]any)
		if !ok {
			return 0
		}
		data = next
	}
	return 0
}

// formatTokens renders a token count compactly (1.2k, 3.4M).
func formatTokens(count int) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fk", float64(count)/1_000)
	default:
		return fmt.Sprintf("%d", count)
	}
}
