package schema

import "testing"

func TestSkillSourceEqualsAndKey(t *testing.T) {
	directory := SkillDirectorySource{Type: "directory", Path: "/a"}
	if !SkillSourceEquals(directory, SkillDirectorySource{Type: "directory", Path: "/a"}) {
		t.Fatalf("expected equal directory sources")
	}
	if SkillSourceEquals(directory, SkillDirectorySource{Type: "directory", Path: "/b"}) {
		t.Fatalf("expected different directory sources")
	}
	if SkillSourceEquals(directory, SkillURLSource{Type: "url", URL: "/a"}) {
		t.Fatalf("expected different source types to differ")
	}
	if got := SkillSourceKey(directory); got != "directory:/a" {
		t.Fatalf("directory key = %q", got)
	}
	if got := SkillSourceKey(SkillURLSource{Type: "url", URL: "https://x"}); got != "url:https://x" {
		t.Fatalf("url key = %q", got)
	}
	embedded := SkillEmbeddedSource{Type: "embedded", Skill: SkillInfo{Name: "docs"}}
	if !SkillSourceEquals(embedded, SkillEmbeddedSource{Type: "embedded", Skill: SkillInfo{Name: "docs"}}) {
		t.Fatalf("expected equal embedded sources")
	}
	if got := SkillSourceKey(embedded); got != "embedded:docs" {
		t.Fatalf("embedded key = %q", got)
	}
}

func TestAgentColorValid(t *testing.T) {
	for _, color := range []AgentColor{"primary", "secondary", "accent", "success", "warning", "error", "info", "#a1B2c3"} {
		if !color.Valid() {
			t.Fatalf("expected %q to be valid", color)
		}
	}
	for _, color := range []AgentColor{"", "purple", "#12345", "#gggggg", "#1234567"} {
		if color.Valid() {
			t.Fatalf("expected %q to be invalid", color)
		}
	}
}

func TestAgentInfoEmpty(t *testing.T) {
	info := AgentInfoEmpty("build")
	if info.ID != "build" || info.Mode != AgentModeAll || info.Hidden {
		t.Fatalf("unexpected empty agent: %+v", info)
	}
	if info.Request.Headers == nil || info.Request.Body == nil {
		t.Fatalf("expected empty request containers")
	}
	if info.Permissions == nil {
		t.Fatalf("expected non-nil permissions")
	}
}
