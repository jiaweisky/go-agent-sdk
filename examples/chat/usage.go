package main

import (
	"fmt"
	"math"
	"strconv"
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
	// priced is set once a turn was priced, so the status line can show a cost
	// only for the models this demo knows the rates of.
	priced bool
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
		u.priced = true
	}
}

// deepseekCost prices one turn with the model's catalog pricing. It returns
// ok=false for models the demo does not know.
func deepseekCost(modelID string, cacheHit, cacheMiss, output float64, at time.Time) (float64, bool) {
	meta, ok := modelMetadataFor(modelID)
	if !ok {
		return 0, false
	}
	pricing := meta.pricing
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

// formatTokens renders a token count compactly (1.2k, 3.4M). It truncates
// rather than rounds, so 999_999 reads as 999.9k instead of 1000.0k.
func formatTokens(count int) string {
	if count < 1_000 {
		return strconv.Itoa(count)
	}
	unit, suffix := 1_000.0, "k"
	if count >= 1_000_000 {
		unit, suffix = 1_000_000.0, "M"
	}
	return fmt.Sprintf("%.1f%s", math.Floor(float64(count)/unit*10)/10, suffix)
}
