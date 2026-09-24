package utils

// Breakpoints tracks how many cache breakpoints a request may still spend.
type Breakpoints struct {
	Remaining int
	Dropped   int
}

// NewBreakpoints returns a breakpoint budget.
func NewBreakpoints(cap int) Breakpoints {
	return Breakpoints{Remaining: cap, Dropped: 0}
}

// TTLBucket maps a TTL in seconds to the provider's bucket name.
func TTLBucket(ttlSeconds *int) *string {
	if ttlSeconds != nil && *ttlSeconds >= 3600 {
		s := "1h"
		return &s
	}
	return nil
}
