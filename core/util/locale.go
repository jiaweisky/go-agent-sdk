package util

import (
	"os"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// LocaleTag is the default locale, resolved the same way
// String.prototype.localeCompare resolves it by default: from the environment
// (LC_ALL, LC_COLLATE, LANG), falling back to root. Callers that need a
// deterministic order pass an explicit tag instead (see ResolveLocale).
var LocaleTag = localeFromEnv()

func localeFromEnv() language.Tag {
	for _, key := range []string{"LC_ALL", "LC_COLLATE", "LANG"} {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		if tag, err := language.Parse(normalizeLocale(value)); err == nil {
			return tag
		}
	}
	return language.Und
}

// ResolveLocale parses an explicit locale name such as "en-US" or "zh_CN.UTF-8".
// An empty or unparseable name yields LocaleTag, so a host can leave the SDK's
// environment-derived default in place.
func ResolveLocale(name string) language.Tag {
	if name == "" {
		return LocaleTag
	}
	if tag, err := language.Parse(normalizeLocale(name)); err == nil {
		return tag
	}
	return LocaleTag
}

func normalizeLocale(value string) string {
	value = strings.SplitN(value, ".", 2)[0]
	return strings.ReplaceAll(value, "_", "-")
}

// CompareLocale mirrors String.prototype.localeCompare with the given locale.
func CompareLocale(left, right string, tag language.Tag) int {
	return collate.New(tag).CompareString(left, right)
}
