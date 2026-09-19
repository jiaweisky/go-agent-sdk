package util

import (
	"strings"
	"testing"
)

func TestChecksumMatchesTS(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"ses_websearch_test": "1awf4n0",
		"ses_abc":            "1qrpf0p",
		"hello world":        "1n91413",
		"测试会话":               "jizlx4",
	}
	for input, want := range cases {
		if got := Checksum(input); got != want {
			t.Fatalf("Checksum(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSampledChecksumMatchesTS(t *testing.T) {
	content := make([]byte, 10000)
	for i := range content {
		content[i] = byte('A' + i%26)
	}
	want := "10000:b0bcdh:zb6491:zvjmdp:4h0291:1m11fbh"
	if got := SampledChecksum(string(content), 5000); got != want {
		t.Fatalf("SampledChecksum = %q, want %q", got, want)
	}
	if got := SampledChecksum("hello world", 5000); got != Checksum("hello world") {
		t.Fatalf("small input should defer to Checksum, got %q", got)
	}
	if got := SampledChecksum("", 5000); got != "" {
		t.Fatalf("empty input = %q", got)
	}
}

func TestBase64URLRoundTrip(t *testing.T) {
	if got := Base64Encode("hello world"); got != "aGVsbG8gd29ybGQ" {
		t.Fatalf("Base64Encode = %q", got)
	}
	if got := Base64Encode("测试 hello"); got != "5rWL6K-VIGhlbGxv" {
		t.Fatalf("Base64Encode unicode = %q", got)
	}
	decoded, err := Base64Decode("5rWL6K-VIGhlbGxv")
	if err != nil {
		t.Fatalf("Base64Decode: %v", err)
	}
	if decoded != "测试 hello" {
		t.Fatalf("Base64Decode = %q", decoded)
	}
	// atob accepts padding and ASCII whitespace.
	if decoded, err := Base64Decode("aGVsbG8gd29ybGQ="); err != nil || decoded != "hello world" {
		t.Fatalf("Base64Decode padded = %q, %v", decoded, err)
	}
	if decoded, err := Base64Decode("aGVs bG8="); err != nil || decoded != "hello" {
		t.Fatalf("Base64Decode whitespace = %q, %v", decoded, err)
	}
	if _, err := Base64Decode("!!!not base64!!!"); err == nil {
		t.Fatalf("expected invalid base64 to error")
	}
}

func TestHashMatchesTS(t *testing.T) {
	got, err := Hash("hello world")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Fatalf("Hash = %q, want %q", got, want)
	}
	if sha1, err := Hash("hello world", "SHA-1"); err != nil || !strings.HasPrefix(sha1, "2aae6c35c94fcfb4") {
		t.Fatalf("SHA-1 = %q, %v", sha1, err)
	}
	// WebCrypto algorithm names are case-insensitive.
	if lower, err := Hash("hello world", "sha-256"); err != nil || lower != got {
		t.Fatalf("lowercase SHA-256 = %q, %v", lower, err)
	}
	if _, err := Hash("x", "MD5"); err == nil {
		t.Fatalf("expected unsupported algorithm to error")
	}
}
