package flag

import (
	"os"
	"testing"
)

func TestTruthy(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"yes", false},
		{"0", false},
		{"", false},
	}
	for _, c := range cases {
		t.Setenv("FLAG_TEST_TRUTHY", c.value)
		if got := Truthy("FLAG_TEST_TRUTHY"); got != c.want {
			t.Fatalf("Truthy(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestEnabledByExperimental(t *testing.T) {
	// Unset key falls back to OPENCODE_EXPERIMENTAL.
	t.Setenv("OPENCODE_EXPERIMENTAL", "1")
	if !enabledByExperimental("FLAG_TEST_UNSET") {
		t.Fatalf("expected experimental fallback true")
	}
	t.Setenv("OPENCODE_EXPERIMENTAL", "0")
	if enabledByExperimental("FLAG_TEST_UNSET") {
		t.Fatalf("expected experimental fallback false")
	}
	// A set key takes precedence over OPENCODE_EXPERIMENTAL.
	t.Setenv("FLAG_TEST_SET", "1")
	if !enabledByExperimental("FLAG_TEST_SET") {
		t.Fatalf("expected set key true")
	}
}

func TestEnvBooleanMatchesEffectConfig(t *testing.T) {
	for _, value := range []string{"true", "yes", "on", "1", "y"} {
		t.Setenv("FLAG_TEST_BOOL", value)
		if !envBoolean("FLAG_TEST_BOOL") {
			t.Fatalf("envBoolean(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"false", "no", "off", "0", "n", "TRUE", "garbage", ""} {
		t.Setenv("FLAG_TEST_BOOL", value)
		if envBoolean("FLAG_TEST_BOOL") {
			t.Fatalf("envBoolean(%q) = true, want false", value)
		}
	}
}

func TestGettersAreEvaluatedAtAccessTime(t *testing.T) {
	t.Setenv("OPENCODE_DISABLE_PROJECT_CONFIG", "1")
	if !Flag.OPENCODE_DISABLE_PROJECT_CONFIG() {
		t.Fatalf("expected OPENCODE_DISABLE_PROJECT_CONFIG true")
	}
	t.Setenv("OPENCODE_PURE", "true")
	if !Flag.OPENCODE_PURE() {
		t.Fatalf("expected OPENCODE_PURE true")
	}
	t.Setenv("OPENCODE_TUI_CONFIG", "/tmp/tui.json")
	if got := Flag.OPENCODE_TUI_CONFIG(); got != "/tmp/tui.json" {
		t.Fatalf("OPENCODE_TUI_CONFIG = %q", got)
	}
	t.Setenv("OPENCODE_CONFIG_DIR", "/tmp/config")
	if got := Flag.OPENCODE_CONFIG_DIR(); got != "/tmp/config" {
		t.Fatalf("OPENCODE_CONFIG_DIR = %q", got)
	}
	t.Setenv("OPENCODE_PERMISSION", `{"edit":"ask"}`)
	if got := Flag.OPENCODE_PERMISSION(); got != `{"edit":"ask"}` {
		t.Fatalf("OPENCODE_PERMISSION = %q", got)
	}
	t.Setenv("OPENCODE_PLUGIN_META_FILE", "/tmp/meta.json")
	if got := Flag.OPENCODE_PLUGIN_META_FILE(); got != "/tmp/meta.json" {
		t.Fatalf("OPENCODE_PLUGIN_META_FILE = %q", got)
	}
}

func TestClientDefaultsToCLI(t *testing.T) {
	t.Setenv("OPENCODE_CLIENT", "")
	if got := Flag.OPENCODE_CLIENT(); got != "" {
		t.Fatalf("OPENCODE_CLIENT set empty = %q, want empty", got)
	}
	t.Setenv("OPENCODE_CLIENT", "placeholder")
	os.Unsetenv("OPENCODE_CLIENT")
	if got := Flag.OPENCODE_CLIENT(); got != "cli" {
		t.Fatalf("OPENCODE_CLIENT unset = %q, want cli", got)
	}
	t.Setenv("OPENCODE_CLIENT", "desktop")
	if got := Flag.OPENCODE_CLIENT(); got != "desktop" {
		t.Fatalf("OPENCODE_CLIENT = %q", got)
	}
}

func TestPlatformFallbacks(t *testing.T) {
	t.Setenv("OPENCODE_DISABLE_FFF", "1")
	if !disableFFF() {
		t.Fatalf("expected disableFFF true when set")
	}
	t.Setenv("OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT", "true")
	if !disableCopyOnSelect() {
		t.Fatalf("expected disableCopyOnSelect true when set")
	}
}
