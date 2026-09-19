package schema

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

const (
	identifierLength = 26
	identifierChars  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var (
	identifierMu            sync.Mutex
	identifierLastTimestamp int64
	identifierCounter       int64
)

func Ascending() string {
	return Create(false)
}

func Descending() string {
	return Create(true)
}

func Create(descending bool, timestamp ...int64) string {
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

	b := make([]byte, identifierLength-12)
	rand.Read(b)
	for i := range b {
		b[i] = identifierChars[int(b[i])%62]
	}

	return timeHex + string(b)
}

func IdentifierTimestamp(id string) int64 {
	prefixLen := -1
	for i, c := range id {
		if c == '_' {
			prefixLen = i
			break
		}
	}
	if prefixLen < 0 || len(id) < prefixLen+13 {
		return 0
	}
	hex := id[prefixLen+1 : prefixLen+13]
	encoded, ok := new(big.Int).SetString(hex, 16)
	if !ok {
		return 0
	}
	return new(big.Int).Div(encoded, big.NewInt(0x1000)).Int64()
}
