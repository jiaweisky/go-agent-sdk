package util

import (
	"regexp"
	"runtime"
	"strings"
	"sync"
)

var matchCache sync.Map

func Match(input, pattern string) bool {
	normalized := strings.ReplaceAll(input, "\\", "/")
	key := pattern

	if cached, ok := matchCache.Load(key); ok {
		return cached.(*regexp.Regexp).MatchString(normalized)
	}

	escaped := strings.ReplaceAll(pattern, "\\", "/")
	escaped = regexp.QuoteMeta(escaped)
	escaped = strings.ReplaceAll(escaped, `\*`, ".*")
	escaped = strings.ReplaceAll(escaped, `\?`, ".")

	if strings.HasSuffix(escaped, " .*") {
		escaped = escaped[:len(escaped)-3] + "( .*)?"
	}

	// The `s` (dotAll) flag is always set, plus `i` (case-insensitive) on
	// Windows.
	flags := "(?s)"
	if runtime.GOOS == "windows" {
		flags = "(?is)"
	}
	re := regexp.MustCompile(flags + "^" + escaped + "$")
	matchCache.Store(key, re)
	return re.MatchString(normalized)
}
