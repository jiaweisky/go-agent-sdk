package util

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"unicode/utf16"
)

func Base64Encode(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func Base64Decode(value string) (string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "-", "+"), "_", "/")
	// atob ignores ASCII whitespace.
	normalized = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		}
		return r
	}, normalized)
	decoded, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(normalized)
		if err != nil {
			return "", err
		}
	}
	return string(decoded), nil
}

// Hash returns content's digest. The default algorithm is SHA-256; pass an empty
// or omitted algorithm to use it.
func Hash(content string, algorithm ...string) (string, error) {
	name := "SHA-256"
	if len(algorithm) > 0 {
		name = algorithm[0]
	}
	var hasher hash.Hash
	switch strings.ToUpper(name) {
	case "SHA-1":
		hasher = sha1.New()
	case "SHA-256":
		hasher = sha256.New()
	case "SHA-384":
		hasher = sha512.New384()
	case "SHA-512":
		hasher = sha512.New()
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %s", name)
	}
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Checksum is an FNV-1a 32-bit hash over UTF-16 code units, rendered as a
// base-36 string. It returns "" for empty input.
func Checksum(content string) string {
	if content == "" {
		return ""
	}
	return checksumUnits(utf16.Encode([]rune(content)))
}

// SampledChecksum hashes large inputs from five sampled windows. The default
// limit is 500_000 UTF-16 code units.
func SampledChecksum(content string, limit ...int) string {
	if content == "" {
		return ""
	}
	maximum := 500_000
	if len(limit) > 0 {
		maximum = limit[0]
	}
	units := utf16.Encode([]rune(content))
	if len(units) <= maximum {
		return checksumUnits(units)
	}

	const size = 4096
	points := []int{
		0,
		len(units) * 25 / 100,
		len(units) * 50 / 100,
		len(units) * 75 / 100,
		len(units) - size,
	}
	hashes := make([]string, len(points))
	for i, point := range points {
		start := point - size/2
		if start < 0 {
			start = 0
		}
		if upper := len(units) - size; start > upper {
			start = upper
		}
		hashes[i] = checksumUnits(units[start : start+size])
	}
	return strconv.Itoa(len(units)) + ":" + strings.Join(hashes, ":")
}

func checksumUnits(units []uint16) string {
	hash := uint32(0x811c9dc5)
	for _, unit := range units {
		hash ^= uint32(unit)
		hash *= 0x01000193
	}
	return strconv.FormatUint(uint64(hash), 36)
}
