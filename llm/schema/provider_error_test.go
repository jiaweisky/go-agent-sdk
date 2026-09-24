package schema

import "testing"

func TestIsContextOverflow(t *testing.T) {
	overflow := []string{
		"This model's maximum context length is 1048576 tokens. However, you requested 1097277 tokens",
		"prompt is too long",
		"request_too_large",
		"input is too long for requested model",
	}
	for _, message := range overflow {
		if !IsContextOverflow(message) {
			t.Errorf("IsContextOverflow(%q) = false, want true", message)
		}
	}

	notOverflow := []string{
		"rate limit exceeded",
		"too many requests",
		"Throttling error: service unavailable",
		"",
	}
	for _, message := range notOverflow {
		if IsContextOverflow(message) {
			t.Errorf("IsContextOverflow(%q) = true, want false", message)
		}
	}
}

func TestIsContextOverflowNoBody(t *testing.T) {
	for _, message := range []string{
		"400 (no body)",
		"413 (no body)",
		"400 status code (no body)",
		"413 status code (no body)",
	} {
		if !IsContextOverflow(message) {
			t.Fatalf("IsContextOverflow(%q) = false, want true", message)
		}
	}
	if IsContextOverflow("200 (no body)") {
		t.Fatal("IsContextOverflow(\"200 (no body)\") = true, want false")
	}
	if IsContextOverflow("rate limit: 400 (no body)") {
		t.Fatal("an exclusion must win over the no-body pattern")
	}
}
