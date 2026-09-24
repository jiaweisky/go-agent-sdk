package id

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

var Prefixes = map[string]string{
	"job":        "job",
	"event":      "evt",
	"session":    "ses",
	"message":    "msg",
	"permission": "per",
	"question":   "que",
	"part":       "prt",
	"pty":        "pty",
	"tool":       "tool",
	"workspace":  "wrk",
}

const length = 26

var (
	mu            sync.Mutex
	lastTimestamp int64
	counter       int64
)

func Ascending(prefix string, given ...string) string {
	return generateID(prefix, "ascending", given...)
}

func Descending(prefix string, given ...string) string {
	return generateID(prefix, "descending", given...)
}

func generateID(prefix, direction string, given ...string) string {
	if len(given) == 0 || given[0] == "" {
		return Create(Prefixes[prefix], direction)
	}
	if !strings.HasPrefix(given[0], Prefixes[prefix]) {
		panic(fmt.Sprintf("ID %s does not start with %s", given[0], Prefixes[prefix]))
	}
	return given[0]
}

func randomBase62(n int) string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%62]
	}
	return string(b)
}

func Create(prefix, direction string, timestamp ...int64) string {
	mu.Lock()
	defer mu.Unlock()

	ts := time.Now().UnixMilli()
	if len(timestamp) > 0 {
		ts = timestamp[0]
	}

	if ts != lastTimestamp {
		lastTimestamp = ts
		counter = 0
	}
	counter++

	now := big.NewInt(ts)
	now.Mul(now, big.NewInt(0x1000))
	now.Add(now, big.NewInt(counter))

	if direction == "descending" {
		now.Not(now)
	}

	mask := new(big.Int).Lsh(big.NewInt(1), 48)
	mask.Sub(mask, big.NewInt(1))
	now.And(now, mask)

	timeHex := fmt.Sprintf("%012x", now)
	return prefix + "_" + timeHex + randomBase62(length-12)
}
