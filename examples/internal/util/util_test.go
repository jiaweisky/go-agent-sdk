package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAPIKey(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	// openai and the unknown-provider fallback route honor OPENAI_API_KEY.
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	if got := resolveAPIKey("openai"); got != "sk-openai" {
		t.Fatalf("openai = %q", got)
	}
	if got := resolveAPIKey(""); got != "sk-openai" {
		t.Fatalf("default = %q", got)
	}
	// anthropic honors ANTHROPIC_API_KEY (not yet set).
	if got := resolveAPIKey("anthropic"); got != "" {
		t.Fatalf("anthropic (unset) = %q", got)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-anthropic")
	if got := resolveAPIKey("anthropic"); got != "sk-anthropic" {
		t.Fatalf("anthropic = %q", got)
	}
	// openai-compatible has no env fallback.
	if got := resolveAPIKey("openai-compatible"); got != "" {
		t.Fatalf("openai-compatible = %q", got)
	}
	// OPENCODE_API_KEY wins for every provider.
	t.Setenv("OPENCODE_API_KEY", "sk-opencode")
	for _, provider := range []string{"openai", "anthropic", "openai-compatible"} {
		if got := resolveAPIKey(provider); got != "sk-opencode" {
			t.Fatalf("%s = %q, want sk-opencode", provider, got)
		}
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	content := "# comment\nOPENCODE_DOTENV_TEST=hello\nQUOTED=\"world\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	t.Setenv("OPENCODE_DOTENV_TEST", "original") // registers restore
	_ = os.Unsetenv("OPENCODE_DOTENV_TEST")

	LoadDotEnv()
	if got := os.Getenv("OPENCODE_DOTENV_TEST"); got != "hello" {
		t.Fatalf("value = %q, want hello", got)
	}
	if got := os.Getenv("QUOTED"); got != "world" {
		t.Fatalf("quoted = %q, want world", got)
	}

	// Variables already in the environment win.
	t.Setenv("QUOTED", "keep")
	LoadDotEnv()
	if got := os.Getenv("QUOTED"); got != "keep" {
		t.Fatalf("override = %q, want keep", got)
	}
}
