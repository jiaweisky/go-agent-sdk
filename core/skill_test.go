package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type stubSkillDiscovery struct {
	urls  map[string][]schema.AbsolutePath
	pulls int
}

func (s *stubSkillDiscovery) Pull(url string) ([]schema.AbsolutePath, error) {
	s.pulls++
	return s.urls[url], nil
}

func writeSkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSkillSourcesDedupAndPrecedence(t *testing.T) {
	tmp := t.TempDir()
	first := filepath.Join(tmp, "first")
	second := filepath.Join(tmp, "second")
	writeSkillFile(t, filepath.Join(first, "review", "SKILL.md"), "---\nname: review\ndescription: First\n---\n# review")
	writeSkillFile(t, filepath.Join(second, "review", "SKILL.md"), "---\nname: review\ndescription: Second\n---\n# review")
	writeSkillFile(t, filepath.Join(first, "foo.md"), "---\nslash: true\n---\n# foo")

	skill := NewSkill(&stubSkillDiscovery{urls: map[string][]schema.AbsolutePath{}})
	if _, err := skill.Transform(func(draft SkillDraft) error {
		draft.Source(schema.SkillDirectorySource{Type: "directory", Path: schema.AbsolutePath(first)})
		draft.Source(schema.SkillDirectorySource{Type: "directory", Path: schema.AbsolutePath(first)})
		draft.Source(schema.SkillDirectorySource{Type: "directory", Path: schema.AbsolutePath(second)})
		if len(draft.List()) != 2 {
			t.Fatalf("draft sources = %d, want 2", len(draft.List()))
		}
		return nil
	}); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(skill.Sources()) != 2 {
		t.Fatalf("sources = %d, want 2", len(skill.Sources()))
	}

	list, err := skill.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Name != "foo" || list[0].Slash == nil || !*list[0].Slash || list[0].Content != "# foo" {
		t.Fatalf("foo = %+v", list[0])
	}
	if list[1].Name != "review" || list[1].Description == nil || *list[1].Description != "Second" {
		t.Fatalf("review = %+v", list[1])
	}
}

func TestSkillURLSourceCachesAndFilters(t *testing.T) {
	tmp := t.TempDir()
	writeSkillFile(t, filepath.Join(tmp, "deploy", "SKILL.md"), "---\nname: deploy\ndescription: Deploy production\n---\n# deploy")

	discovery := &stubSkillDiscovery{urls: map[string][]schema.AbsolutePath{
		"https://example.test/skills/": {schema.AbsolutePath(tmp)},
	}}
	skill := NewSkill(discovery)
	if _, err := skill.Transform(func(draft SkillDraft) error {
		draft.Source(schema.SkillURLSource{Type: "url", URL: "https://example.test/skills/"})
		return nil
	}); err != nil {
		t.Fatalf("transform: %v", err)
	}

	first, err := skill.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first) != 1 || first[0].Name != "deploy" {
		t.Fatalf("list = %+v", first)
	}
	if _, err := skill.List(); err != nil {
		t.Fatalf("second list: %v", err)
	}
	if discovery.pulls != 1 {
		t.Fatalf("pulls = %d, want 1", discovery.pulls)
	}

	agent := schema.AgentInfoEmpty("reviewer")
	agent.Permissions = schema.PermissionRuleset{{Action: "skill", Resource: "deploy", Effect: "deny"}}
	if got := SkillAvailable(first, agent); len(got) != 0 {
		t.Fatalf("available = %+v", got)
	}
}
