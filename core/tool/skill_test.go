package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/llm/schema"
)

type stubSkillLister struct {
	skills []core.SkillInfo
}

func (s stubSkillLister) List() ([]core.SkillInfo, error) {
	return s.skills, nil
}

func writeToolFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func settleSkillTool(t *testing.T, deps SkillToolDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterSkill(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_skill_tool_test",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call-skill",
			Name:  SkillName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestSkillToolLoadsModelContent(t *testing.T) {
	tmp := t.TempDir()
	directory := filepath.Join(tmp, "effect")
	location := filepath.Join(directory, "SKILL.md")
	reference := filepath.Join(directory, "reference.md")
	writeToolFile(t, location, "unused")
	writeToolFile(t, reference, "reference")

	description := "Use Effect"
	info := core.SkillInfo{
		Name:        "effect",
		Description: &description,
		Location:    core.AbsolutePath(location),
		Content:     "# Effect\n\nGuidance",
	}
	permission := &bashTestPermission{}
	result := settleSkillTool(t, SkillToolDeps{
		Skills:     stubSkillLister{skills: []core.SkillInfo{info}},
		Permission: permission,
	}, map[string]any{"name": "effect"})

	expected := SkillToModelOutput(info, []string{reference})
	if result.Result.Type != "text" || result.Result.Value != expected {
		t.Fatalf("result = %+v", result.Result)
	}
	if !strings.Contains(expected, "Base directory for this skill: "+directory) {
		t.Fatalf("missing base directory in output")
	}
	if len(permission.assertions) != 1 {
		t.Fatalf("assertions = %d", len(permission.assertions))
	}
	assertion := permission.assertions[0]
	if assertion.Action != SkillName || len(assertion.Resources) != 1 || assertion.Resources[0] != "effect" ||
		len(assertion.Save) != 1 || assertion.Save[0] != "effect" {
		t.Fatalf("assertion = %+v", assertion)
	}
}

func TestSkillToolMissingAndDenied(t *testing.T) {
	tmp := t.TempDir()
	location := filepath.Join(tmp, "effect", "SKILL.md")
	writeToolFile(t, location, "unused")
	info := core.SkillInfo{Name: "effect", Location: core.AbsolutePath(location), Content: "# Effect"}

	missing := settleSkillTool(t, SkillToolDeps{
		Skills:     stubSkillLister{skills: []core.SkillInfo{info}},
		Permission: &bashTestPermission{},
	}, map[string]any{"name": "missing"})
	if missing.Result.Type != "error" || missing.Result.Value != "Unable to load skill missing" {
		t.Fatalf("missing = %+v", missing.Result)
	}

	denied := settleSkillTool(t, SkillToolDeps{
		Skills:     stubSkillLister{skills: []core.SkillInfo{info}},
		Permission: &bashTestPermission{denyAction: SkillName},
	}, map[string]any{"name": "effect"})
	if denied.Result.Type != "error" || denied.Result.Value != "Unable to load skill effect" {
		t.Fatalf("denied = %+v", denied.Result)
	}
}

func TestSkillToolFlatSkillHasNoFiles(t *testing.T) {
	tmp := t.TempDir()
	location := filepath.Join(tmp, "public.md")
	writeToolFile(t, location, "public")
	flat := core.SkillInfo{Name: "public", Location: core.AbsolutePath(location), Content: "Public"}

	result := settleSkillTool(t, SkillToolDeps{
		Skills:     stubSkillLister{skills: []core.SkillInfo{flat}},
		Permission: &bashTestPermission{},
	}, map[string]any{"name": "public"})

	expected := SkillToModelOutput(flat, nil)
	if result.Result.Type != "text" || result.Result.Value != expected {
		t.Fatalf("result = %+v", result.Result)
	}
}
