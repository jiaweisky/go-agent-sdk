package utils

type Breakpoints struct {
	Remaining int
	Dropped   int
}

func NewBreakpoints(cap int) Breakpoints {
	return Breakpoints{Remaining: cap, Dropped: 0}
}

func TTLBucket(ttlSeconds *int) *string {
	if ttlSeconds != nil && *ttlSeconds >= 3600 {
		s := "1h"
		return &s
	}
	return nil
}
