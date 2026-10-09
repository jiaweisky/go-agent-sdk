package core

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestRepositoryParse(t *testing.T) {
	cases := []struct {
		input string
		host  string
		path  string
	}{
		{"github:owner/repo", "github.com", "owner/repo"},
		{"owner/repo", "github.com", "owner/repo"},
		{"https://github.com/owner/repo.git", "github.com", "owner/repo"},
		{"git@github.com:owner/repo.git", "github.com", "owner/repo"},
		{"https://gitlab.com/owner/repo", "gitlab.com", "owner/repo"},
	}
	for _, tc := range cases {
		reference := RepositoryParse(tc.input)
		if reference == nil || reference.Host != tc.host || reference.Path != tc.path {
			t.Fatalf("parse(%q) = %+v", tc.input, reference)
		}
	}
	if RepositoryParse("") != nil {
		t.Fatal("expected empty input to parse to nil")
	}
}

func TestRepositoryParseGitHubMirror(t *testing.T) {
	// A shorthand is routed through the mirror when the caller asks for one.
	mirrored := RepositoryParse("github:owner/repo", WithGitHubBaseURL("https://mirror.example.com/"))
	if mirrored == nil || mirrored.Remote != "https://mirror.example.com/owner/repo.git" {
		t.Fatalf("mirrored remote = %+v", mirrored)
	}
	// An explicit github.com URL is mirrored too, so origin and clone target stay
	// consistent.
	explicit := RepositoryParse("https://github.com/owner/repo.git", WithGitHubBaseURL("https://mirror.example.com/"))
	if explicit == nil || explicit.Remote != "https://mirror.example.com/owner/repo.git" {
		t.Fatalf("explicit remote = %+v", explicit)
	}
	// Other hosts are never rewritten.
	other := RepositoryParse("https://gitlab.com/owner/repo.git", WithGitHubBaseURL("https://mirror.example.com/"))
	if other == nil || other.Remote != "https://gitlab.com/owner/repo.git" {
		t.Fatalf("other remote = %+v", other)
	}
	// Without the option the canonical URL is used.
	if got := RepositoryParse("github:owner/repo").Remote; got != "https://github.com/owner/repo.git" {
		t.Fatalf("canonical remote = %q", got)
	}
}

func TestRepositoryCacheForwardsParseOptions(t *testing.T) {
	cache := NewRepositoryCache(Git{}, t.TempDir(), WithGitHubBaseURL("https://mirror.example.com/"))
	options := cache.repositoryOptions()
	if len(options) != 1 {
		t.Fatalf("options = %v", options)
	}
	config := RepositoryOptions{}
	options[0](&config)
	if config.GitHubBaseURL != "https://mirror.example.com/" {
		t.Fatalf("GitHubBaseURL = %q", config.GitHubBaseURL)
	}
	var nilCache *RepositoryCache
	if options := nilCache.repositoryOptions(); options != nil {
		t.Fatalf("nil cache options = %v", options)
	}
}

func TestRepositoryValidateBranch(t *testing.T) {
	if err := RepositoryValidateBranch("feature/x"); err != nil {
		t.Fatalf("valid branch rejected: %v", err)
	}
	if err := RepositoryValidateBranch("-bad"); err == nil {
		t.Fatal("expected leading dash to be rejected")
	}
	if err := RepositoryValidateBranch("a..b"); err == nil {
		t.Fatal("expected .. to be rejected")
	}
}

func TestReferenceService(t *testing.T) {
	global := NewGlobal(t.TempDir(), "/home")
	reference := NewReference(global, nil, nil)
	if _, err := reference.Transform(func(draft ReferenceDraft) error {
		draft.Add("local", ReferenceLocalSource{Type: "local", Path: schema.AbsolutePath("/tmp/x")})
		draft.Add("remote", ReferenceGitSource{Type: "git", Repository: "github:owner/repo"})
		draft.Add("ignored", ReferenceGitSource{Type: "git", Repository: "not a repo"})
		return nil
	}); err != nil {
		t.Fatalf("transform: %v", err)
	}
	infos := reference.List()
	if len(infos) != 2 {
		t.Fatalf("infos = %+v", infos)
	}
	byName := map[string]ReferenceInfo{}
	for _, info := range infos {
		byName[info.Name] = info
	}
	if byName["local"].Path != schema.AbsolutePath("/tmp/x") {
		t.Fatalf("local = %+v", byName["local"])
	}
	if byName["remote"].Source.ReferenceSourceType() != "git" {
		t.Fatalf("remote = %+v", byName["remote"])
	}
}
