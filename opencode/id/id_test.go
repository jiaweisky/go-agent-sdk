package id

import (
	"regexp"
	"strings"
	"testing"
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func TestCreateFormat(t *testing.T) {
	value := Create("ses", "ascending")
	if !strings.HasPrefix(value, "ses_") {
		t.Fatalf("value = %q, want a ses_ prefix", value)
	}
	// prefix + "_" + 12 hex time bytes + 14 base62 random bytes.
	if want := len("ses") + 1 + 12 + 14; len(value) != want {
		t.Fatalf("len(%q) = %d, want %d", value, len(value), want)
	}
	time := value[len("ses")+1 : len("ses")+1+12]
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(time) {
		t.Fatalf("time bytes = %q, want 12 lowercase hex digits", time)
	}
	for _, c := range value[len("ses")+1+12:] {
		if !strings.ContainsRune(base62, c) {
			t.Fatalf("random suffix %q contains %q", value[len("ses")+1+12:], c)
		}
	}
}

func TestAscendingUsesPrefixTable(t *testing.T) {
	cases := map[string]string{
		"session": "ses",
		"message": "msg",
		"event":   "evt",
		"tool":    "tool",
	}
	for prefix, want := range cases {
		if got := Ascending(prefix); !strings.HasPrefix(got, want+"_") {
			t.Fatalf("Ascending(%q) = %q, want a %s_ prefix", prefix, got, want)
		}
	}
}

func TestGivenIDPassesThrough(t *testing.T) {
	for _, given := range []string{"ses_existing", "ses_"} {
		if got := Ascending("session", given); got != given {
			t.Fatalf("Ascending(session, %q) = %q", given, got)
		}
		if got := Descending("session", given); got != given {
			t.Fatalf("Descending(session, %q) = %q", given, got)
		}
	}
}

func TestGivenEmptyStringGeneratesNewID(t *testing.T) {
	if got := Ascending("session", ""); got == "" || !strings.HasPrefix(got, "ses_") {
		t.Fatalf("Ascending(session, \"\") = %q, want a new ses_ id", got)
	}
}

func TestGivenIDWithWrongPrefixPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want a panic for a mismatched prefix")
		}
	}()
	Ascending("session", "msg_123")
}

func TestCounterBreaksTimestampTies(t *testing.T) {
	const ts = int64(1_700_000_000_000)
	first := Create("msg", "ascending", ts)
	second := Create("msg", "ascending", ts)
	if first == second {
		t.Fatalf("two ids at the same timestamp are identical: %q", first)
	}
	if first[len("msg")+1:len("msg")+13] == second[len("msg")+1:len("msg")+13] {
		t.Fatalf("time bytes did not advance within the same millisecond")
	}
}

func TestTimestampRoundTripWithinMaskWindow(t *testing.T) {
	const ts = int64(60_000_000_000)
	if got := Timestamp(Create("ses", "ascending", ts)); got != ts {
		t.Fatalf("Timestamp = %d, want %d", got, ts)
	}
}

func TestDescendingOrdersNewestFirst(t *testing.T) {
	const base = int64(1_000_000_000)
	early := Create("ses", "descending", base)
	late := Create("ses", "descending", base+1)
	if !(early > late) {
		t.Fatalf("descending ids are not ordered newest-first: early=%q late=%q", early, late)
	}
}

func TestTimestampMalformed(t *testing.T) {
	for _, value := range []string{"", "ses_short", "no-underscore", "ses_zzzzzzzzzzzz"} {
		if got := Timestamp(value); got != 0 {
			t.Fatalf("Timestamp(%q) = %d, want 0", value, got)
		}
	}
}
