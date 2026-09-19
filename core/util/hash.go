package util

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
)

func HashFast(input []byte) string {
	sum := sha1.Sum(input)
	return hex.EncodeToString(sum[:])
}

func HashFastString(input string) string {
	return HashFast([]byte(input))
}

func HashSha256(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

func HashSha256String(input string) string {
	return HashSha256([]byte(input))
}
