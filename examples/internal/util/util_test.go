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

func TestProjectRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectRoot(sub); got != repo {
		t.Fatalf("projectRoot(%s) = %s, want %s", sub, got, repo)
	}

	plain := t.TempDir()
	if got, want := projectRoot(plain), filepath.VolumeName(plain)+string(filepath.Separator); got != want {
		t.Fatalf("projectRoot without git = %q, want %q", got, want)
	}
}

func TestLoadDotEnvSearchesToProjectRoot(t *testing.T) {
	const key = "EXAMPLE_DOTENV_SEARCH"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte(key+"=root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	LoadDotEnv()
	if got := os.Getenv(key); got != "root" {
		t.Fatalf("%s = %q, want root (ancestor .env)", key, got)
	}

	// A nearer .env wins and the search stops there.
	os.Unsetenv(key)
	if err := os.WriteFile(filepath.Join(sub, ".env"), []byte(key+"=near\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	LoadDotEnv()
	if got := os.Getenv(key); got != "near" {
		t.Fatalf("%s = %q, want near (nearest .env)", key, got)
	}
}
