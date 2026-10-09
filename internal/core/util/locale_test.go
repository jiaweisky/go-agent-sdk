package util

import (
	"sort"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

func TestCompareLocaleOrdersASCII(t *testing.T) {
	names := []string{"b", "a", "c"}
	sort.Slice(names, func(i, j int) bool { return CompareLocale(names[i], names[j], language.Und) < 0 })
	if strings.Join(names, ",") != "a,b,c" {
		t.Fatalf("names = %v", names)
	}
}

func TestResolveLocale(t *testing.T) {
	// An explicit name wins, accepting the POSIX form too.
	if tag := ResolveLocale("en-US"); tag.String() != "en-US" {
		t.Fatalf("en-US tag = %v", tag)
	}
	if tag := ResolveLocale("zh_CN.UTF-8"); tag.String() != "zh-CN" {
		t.Fatalf("POSIX tag = %v", tag)
	}
	// Empty and unparseable names keep the environment-derived default.
	original := LocaleTag
	t.Cleanup(func() { LocaleTag = original })
	t.Setenv("LC_ALL", "de-DE")
	LocaleTag = localeFromEnv()
	if tag := ResolveLocale(""); tag != LocaleTag {
		t.Fatalf("empty tag = %v, want the default %v", tag, LocaleTag)
	}
	if tag := ResolveLocale("!!!"); tag != LocaleTag {
		t.Fatalf("invalid tag = %v, want the default %v", tag, LocaleTag)
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
