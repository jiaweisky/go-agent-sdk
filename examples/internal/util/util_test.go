package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err := DefaultCatalog.DefaultModel()
	if err == nil {
		t.Fatal("DefaultModel should error without any provider key")
	}
	// The hint is derived from the catalog, so every provider key is named.
	for _, env := range DefaultCatalog.providerKeyEnvs() {
		if !strings.Contains(err.Error(), env) {
			t.Fatalf("error %q missing %s", err, env)
		}
	}

	t.Setenv("DEEPSEEK_API_KEY", "test")
	got, err := DefaultCatalog.DefaultModel()
	if err != nil || got != "deepseek-v4-flash" {
		t.Fatalf("DefaultModel = %q, %v; want deepseek-v4-flash", got, err)
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	content := "# comment\nAGENT_DOTENV_TEST=hello\nQUOTED=\"world\"\n"
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

	t.Setenv("AGENT_DOTENV_TEST", "original") // registers restore
	_ = os.Unsetenv("AGENT_DOTENV_TEST")

	LoadDotEnv()
	if got := os.Getenv("AGENT_DOTENV_TEST"); got != "hello" {
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
