package instructions

import (
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/core/systemcontext"
)

func TestEnvironmentSourceRendersEnvBlock(t *testing.T) {
	gen, err := systemcontext.Initialize(EnvironmentSource(Environment{
		WorkingDirectory: "/work",
		WorkspaceRoot:    "/work",
		IsGitRepo:        true,
		Platform:         "linux",
		TempDirectory:    "/root/tmp",
	}))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	for _, want := range []string{
		"Here is some useful information about the environment you are running in:",
		"<env>",
		"Working directory: /work",
		"Workspace root folder: /work",
		"Is directory a git repo: yes",
		"Platform: linux",
		"Prefer /root/tmp over generic system temporary directories such as /tmp",
		"</env>",
	} {
		if !strings.Contains(gen.Baseline, want) {
			t.Errorf("environment block missing %q:\n%s", want, gen.Baseline)
		}
	}
}

func TestEnvironmentSourceNotGit(t *testing.T) {
	gen, err := systemcontext.Initialize(EnvironmentSource(Environment{Platform: "darwin"}))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !strings.Contains(gen.Baseline, "Is directory a git repo: no") {
		t.Fatalf("non-git environment block:\n%s", gen.Baseline)
	}
}

func TestDateSource(t *testing.T) {
	now := time.Date(2026, time.October, 13, 9, 30, 0, 0, time.UTC)
	gen, err := systemcontext.Initialize(Date(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !strings.Contains(gen.Baseline, "Today's date: "+now.Format("Mon Jan 02 2006")) {
		t.Fatalf("date source baseline:\n%s", gen.Baseline)
	}
}

func TestDateSourceUpdatesAcrossDays(t *testing.T) {
	day1 := time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	clock := day1
	source := Date(func() time.Time { return clock })

	gen, err := systemcontext.Initialize(source)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	clock = day2
	result, err := systemcontext.Reconcile(source, gen.Snapshot)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Tag != "Updated" {
		t.Fatalf("tag = %q, want Updated", result.Tag)
	}
	if !strings.Contains(result.Text, "Today's date is now: "+day2.Format("Mon Jan 02 2006")) {
		t.Fatalf("update text = %q", result.Text)
	}
}
