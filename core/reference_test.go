package core

import (
	"testing"

	"github.com/user/go-agent-sdk/schema"
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
