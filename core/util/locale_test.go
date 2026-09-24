package util

import (
	"sort"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

func TestCompareLocaleOrdersASCII(t *testing.T) {
	names := []string{"b", "a", "c"}
	sort.Slice(names, func(i, j int) bool { return CompareLocale(names[i], names[j]) < 0 })
	if strings.Join(names, ",") != "a,b,c" {
		t.Fatalf("names = %v", names)
	}
}

func TestLocaleFromEnv(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "")
	if tag := localeFromEnv(); tag.String() != "en-US" {
		t.Fatalf("LC_ALL tag = %v", tag)
	}

	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	if tag := localeFromEnv(); tag.String() != "de-DE" {
		t.Fatalf("LANG tag = %v", tag)
	}

	t.Setenv("LANG", "")
	if tag := localeFromEnv(); tag != language.Und {
		t.Fatalf("empty tag = %v", tag)
	}
}
