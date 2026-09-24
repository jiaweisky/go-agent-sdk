package core

import (
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

func TestApplyDefaultAgents(t *testing.T) {
	agents := NewAgent()
	global := NewGlobal("/data", "/home")
	location := &Location{LocationInfo: schema.LocationInfo{Directory: schema.AbsolutePath("/work")}}
	if err := ApplyDefaultAgents(agents, location, global); err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, id := range []string{"build", "plan", "general", "explore", "compaction", "title", "summary"} {
		if agents.Get(schema.AgentID(id)) == nil {
			t.Fatalf("missing default agent %q", id)
		}
	}

	build := agents.Get(AgentDefaultID)
	if build.Mode != schema.AgentModePrimary || build.Description == nil {
		t.Fatalf("build = %+v", build)
	}
	if build.System == nil || *build.System != agentBuildSystem {
		t.Fatalf("build system = %v", build.System)
	}
	if got := PermissionEvaluate("question", "*", build.Permissions).Effect; got != PermissionEffectAllow {
		t.Fatalf("build question = %v", got)
	}

	plan := agents.Get(schema.AgentID("plan"))
	if got := PermissionEvaluate("edit", "*", plan.Permissions).Effect; got != PermissionEffectDeny {
		t.Fatalf("plan edit = %v", got)
	}

	explore := agents.Get(schema.AgentID("explore"))
	if explore.Mode != schema.AgentModeSubagent {
		t.Fatalf("explore mode = %v", explore.Mode)
	}
	if got := PermissionEvaluate("grep", "*", explore.Permissions).Effect; got != PermissionEffectAllow {
		t.Fatalf("explore grep = %v", got)
	}
	if got := PermissionEvaluate("edit", "*", explore.Permissions).Effect; got != PermissionEffectDeny {
		t.Fatalf("explore edit = %v", got)
	}

	for _, id := range []string{"compaction", "title", "summary"} {
		if !agents.Get(schema.AgentID(id)).Hidden {
			t.Fatalf("%s should be hidden", id)
		}
	}

	if agents.Default().ID != AgentDefaultID {
		t.Fatalf("default = %v", agents.Default().ID)
	}
}
