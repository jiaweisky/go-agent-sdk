package util

import (
	"os"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// LocaleTag resolves the runtime locale the same way
// String.prototype.localeCompare does by default: from the environment,
// falling back to root.
var LocaleTag = localeFromEnv()

func localeFromEnv() language.Tag {
	for _, key := range []string{"LC_ALL", "LC_COLLATE", "LANG"} {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		value = strings.SplitN(value, ".", 2)[0]
		value = strings.ReplaceAll(value, "_", "-")
		if tag, err := language.Parse(value); err == nil {
			return tag
		}
	}
	return language.Und
}

// CompareLocale mirrors String.prototype.localeCompare with the runtime locale.
func CompareLocale(left, right string) int {
	return collate.New(LocaleTag).CompareString(left, right)
}
