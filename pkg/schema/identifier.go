package schema

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	identifierLength       = 26
	identifierTimeHexWidth = 12
	identifierChars        = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var (
	identifierMu            sync.Mutex
	identifierLastTimestamp int64
	identifierCounter       int64
)

// Ascending returns an identifier whose lexical order matches creation order.
func Ascending() string {
	return CreateIdentifier(false)
}

// Descending returns an identifier whose lexical order is reversed.
func Descending() string {
	return CreateIdentifier(true)
}

// generateID returns a fresh identifier with prefix + "_".
func generateID(prefix string, descending bool) string {
	if descending {
		return prefix + "_" + Descending()
	}
	return prefix + "_" + Ascending()
}

// validateID checks that an existing id carries the expected prefix.
func validateID(prefix, id string) error {
	if !strings.HasPrefix(id, prefix) {
		return fmt.Errorf("ID %q does not start with %q", id, prefix)
	}
	return nil
}

// CreateIdentifier returns a time-sortable identifier. Its first 48 bits encode
// the millisecond timestamp and a per-millisecond counter; descending inverts
// them so newer identifiers sort first.
func CreateIdentifier(descending bool, timestamp ...int64) string {
	identifierMu.Lock()
	defer identifierMu.Unlock()

	ts := time.Now().UnixMilli()
	if len(timestamp) > 0 {
		ts = timestamp[0]
	}

	if ts != identifierLastTimestamp {
		identifierLastTimestamp = ts
		identifierCounter = 0
	}
	identifierCounter++

	value := big.NewInt(ts)
	value.Mul(value, big.NewInt(0x1000))
	value.Add(value, big.NewInt(identifierCounter))
	if descending {
		value.Not(value)
	}

	mask := new(big.Int).Lsh(big.NewInt(1), 48)
	mask.Sub(mask, big.NewInt(1))
	value.And(value, mask)

	timeHex := fmt.Sprintf("%012x", value)

	b := make([]byte, identifierLength-identifierTimeHexWidth)
	rand.Read(b)
	for i := range b {
		b[i] = identifierChars[int(b[i])%len(identifierChars)]
	}

	return timeHex + string(b)
}
