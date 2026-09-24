package main

import (
	"math"
	"testing"
	"time"
)

func TestDeepseekPeak(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		// Beijing = UTC+8.
		{"weekday 10:00", time.Date(2024, 1, 2, 2, 0, 0, 0, time.UTC), true},
		{"weekday 15:00", time.Date(2024, 1, 2, 7, 0, 0, 0, time.UTC), true},
		{"weekday 13:00 (lunch)", time.Date(2024, 1, 2, 5, 0, 0, 0, time.UTC), false},
		{"weekday 20:00", time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC), false},
		{"weekend 10:00", time.Date(2024, 1, 6, 2, 0, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		if got := deepseekPeak(c.at); got != c.want {
			t.Errorf("%s: deepseekPeak = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDeepseekCost(t *testing.T) {
	peak := time.Date(2024, 1, 2, 2, 0, 0, 0, time.UTC) // Tue 10:00 Beijing
	off := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)

	// flash peak: 0.04 + 2 + 8 = 10.04 CNY for 1M of each.
	cost, ok := deepseekCost("deepseek-v4-flash", 1_000_000, 1_000_000, 1_000_000, peak)
	if !ok || math.Abs(cost-10.04) > 1e-9 {
		t.Fatalf("flash peak cost = %v ok=%v, want 10.04", cost, ok)
	}
	// Off-peak is half.
	if cost, _ := deepseekCost("deepseek-v4-flash", 1_000_000, 1_000_000, 1_000_000, off); math.Abs(cost-5.02) > 1e-9 {
		t.Fatalf("flash off-peak cost = %v, want 5.02", cost)
	}
	// pro peak: 0.30 + 9 + 27 = 36.3.
	if cost, _ := deepseekCost("deepseek-v4-pro", 1_000_000, 1_000_000, 1_000_000, peak); math.Abs(cost-36.3) > 1e-9 {
		t.Fatalf("pro peak cost = %v, want 36.3", cost)
	}
	// Unknown models are not priced.
	if _, ok := deepseekCost("gpt-4o", 1, 1, 1, peak); ok {
		t.Fatal("unknown model should not be priced")
	}
	// Another vendor's "pro" model must not inherit DeepSeek's schedule.
	for _, model := range []string{"gemini-2.5-pro", "gpt-5-pro", "claude-opus-pro"} {
		if _, ok := deepseekCost(model, 1_000_000, 1_000_000, 1_000_000, peak); ok {
			t.Errorf("%s should not be priced with the DeepSeek schedule", model)
		}
	}
	// A DeepSeek model outside the priced families gets no invented rate.
	if _, ok := deepseekCost("deepseek-chat", 1, 1, 1, peak); ok {
		t.Fatal("an unlisted deepseek model should not be priced")
	}
}

func TestChatUsageUnpricedModel(t *testing.T) {
	var usage chatUsage
	raw := map[string]map[string]any{"openai": {
		"prompt_cache_hit_tokens":  float64(100),
		"prompt_cache_miss_tokens": float64(200),
		"completion_tokens":        float64(50),
	}}
	usage.addUsage("gpt-4o", raw, time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))

	if usage.tokens() != 350 {
		t.Fatalf("tokens = %d, want 350", usage.tokens())
	}
	if usage.priced || usage.cost != 0 {
		t.Fatalf("usage = %+v, want no cost for an unpriced model", usage)
	}
}

func TestChatUsageAccumulates(t *testing.T) {
	var usage chatUsage
	raw := map[string]map[string]any{"openai": {
		"prompt_cache_hit_tokens":  float64(100),
		"prompt_cache_miss_tokens": float64(200),
		"completion_tokens":        float64(50),
	}}
	usage.addUsage("deepseek-v4-flash", raw, time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))

	if usage.cacheHit != 100 || usage.cacheMiss != 200 || usage.output != 50 || usage.tokens() != 350 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.cost <= 0 {
		t.Fatalf("cost = %v, want > 0", usage.cost)
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int]string{
		500:       "500",
		1500:      "1.5k",
		2_000_000: "2.0M",
		// Truncated, not rounded up into the next unit.
		999_999:   "999.9k",
		1_999_999: "1.9M",
	}
	for in, want := range cases {
		if got := formatTokens(in); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", in, got, want)
		}
	}
}
