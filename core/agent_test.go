package core

import (
	"reflect"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func strPtr(value string) *string { return &value }

func TestAgentStartsEmpty(t *testing.T) {
	agent := NewAgent()
	if got := agent.All(); len(got) != 0 {
		t.Fatalf("all = %v", got)
	}
	if agent.Get(AgentDefaultID) != nil {
		t.Fatalf("expected build agent to be absent")
	}
}

func TestAgentTransformAndDispose(t *testing.T) {
	agent := NewAgent()
	id := schema.AgentID("reviewer")
	description := "Old description"
	hidden := true

	dispose, err := agent.Transform(func(draft AgentDraft) error {
		draft.Update(id, func(info *schema.AgentInfo) {
			info.Description = &description
			info.Mode = schema.AgentModeSubagent
			info.Hidden = hidden
		})
		return nil
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	info := agent.Get(id)
	if info == nil || info.Description == nil || *info.Description != "Old description" || info.Mode != schema.AgentModeSubagent {
		t.Fatalf("unexpected agent: %+v", info)
	}
	if got := agent.All(); len(got) != 1 || got[0].ID != id {
		t.Fatalf("all = %+v", got)
	}

	description = "New description"
	hidden = false
	if err := agent.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	info = agent.Get(id)
	if info.Description == nil || *info.Description != "New description" || info.Hidden {
		t.Fatalf("after reload: %+v", info)
	}

	if err := dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if agent.Get(id) != nil {
		t.Fatalf("expected agent removed after dispose")
	}
	if err := dispose(); err != nil {
		t.Fatalf("second dispose: %v", err)
	}
}

func TestAgentCreatesWithDefaultsAndRemoves(t *testing.T) {
	agent := NewAgent()
	id := schema.AgentID("custom")
	if _, err := agent.Transform(func(draft AgentDraft) error {
		draft.Update(id, func(*schema.AgentInfo) {})
		return nil
	}); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if got, want := agent.Get(id), schema.AgentInfoEmpty(id); !reflect.DeepEqual(got, &want) {
		t.Fatalf("agent = %+v, want %+v", got, want)
	}
	if _, err := agent.Transform(func(draft AgentDraft) error {
		draft.Remove(id)
		return nil
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if agent.Get(id) != nil {
		t.Fatalf("expected agent removed")
	}
}

func TestAgentDefaultSelection(t *testing.T) {
	agent := NewAgent()
	add := func(id schema.AgentID, mode string, hidden bool) {
		t.Helper()
		if _, err := agent.Transform(func(draft AgentDraft) error {
			draft.Update(id, func(info *schema.AgentInfo) {
				info.Mode = mode
				info.Hidden = hidden
			})
			return nil
		}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	add("sub", schema.AgentModeSubagent, false)
	add("hidden", schema.AgentModePrimary, true)
	if got := agent.Default(); got != nil {
		t.Fatalf("expected no selectable default, got %+v", got)
	}

	add("first", schema.AgentModePrimary, false)
	if got := agent.Default(); got == nil || got.ID != "first" {
		t.Fatalf("first selectable default = %+v", got)
	}

	add("build", schema.AgentModePrimary, false)
	if got := agent.Default(); got == nil || got.ID != "build" {
		t.Fatalf("build should win, got %+v", got)
	}

	if _, err := agent.Transform(func(draft AgentDraft) error {
		configured := schema.AgentID("configured")
		draft.Update(configured, func(info *schema.AgentInfo) { info.Mode = schema.AgentModePrimary })
		draft.Default(&configured)
		return nil
	}); err != nil {
		t.Fatalf("configured default: %v", err)
	}
	if got := agent.Default(); got == nil || got.ID != "configured" {
		t.Fatalf("configured default = %+v", got)
	}

	if got := agent.Resolve(strPtr("hidden")); got == nil || got.ID != "hidden" {
		t.Fatalf("resolve by id = %+v", got)
	}
	if got := agent.Resolve(nil); got == nil || got.ID != "configured" {
		t.Fatalf("resolve nil = %+v", got)
	}

	selection := agent.Select(strPtr("first"))
	if selection.ID != "first" || selection.Info == nil || selection.Info.ID != "first" {
		t.Fatalf("select by id = %+v", selection)
	}
	selection = agent.Select(nil)
	if selection.ID != "configured" || selection.Info == nil {
		t.Fatalf("select nil = %+v", selection)
	}
}

func TestAgentSelectFallsBackToBuildID(t *testing.T) {
	agent := NewAgent()
	selection := agent.Select(nil)
	if selection.ID != AgentDefaultID || selection.Info != nil {
		t.Fatalf("selection = %+v", selection)
	}
}
