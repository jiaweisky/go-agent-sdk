// Package instructions holds the built-in system-context sources injected into
// the model's prompt: the environment block and today's date.
package instructions

import (
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/core/systemcontext"
)

// Environment is the environment the environment source renders.
type Environment struct {
	WorkingDirectory string
	WorkspaceRoot    string
	IsGitRepo        bool
	Platform         string
	// TempDirectory is the directory the model is told to prefer for temporary
	// files (over generic system temporary directories).
	TempDirectory string
}

// Date is the core/date source. It re-reads the clock on every load, so a long
// session reports the current day.
func Date(now func() time.Time) systemcontext.SystemContext {
	return systemcontext.Make(systemcontext.Source[string]{
		Key:      "core/date",
		Load:     func() (string, bool, error) { return renderDate(now()), true, nil },
		Baseline: func(date string) string { return "Today's date: " + date },
		Update:   func(_, current string) string { return "Today's date is now: " + current },
		Removed:  func(string) string { return "Today's date is no longer available." },
	})
}

func renderDate(now time.Time) string { return now.Format("Mon Jan 02 2006") }

// EnvironmentSource is the core/environment source: the <env> block, including
// the preferred temporary directory.
func EnvironmentSource(env Environment) systemcontext.SystemContext {
	return systemcontext.Make(systemcontext.Source[string]{
		Key:  "core/environment",
		Load: func() (string, bool, error) { return renderEnvironment(env), true, nil },
		Baseline: func(block string) string {
			return "Here is some useful information about the environment you are running in:\n" + block
		},
		Update: func(_, current string) string {
			return "The environment you are running in is now:\n" + current
		},
		Removed: func(string) string {
			return "The environment information is no longer available."
		},
	})
}

func renderEnvironment(env Environment) string {
	gitRepo := "no"
	if env.IsGitRepo {
		gitRepo = "yes"
	}
	return strings.Join([]string{
		"<env>",
		"  Working directory: " + env.WorkingDirectory,
		"  Workspace root folder: " + env.WorkspaceRoot,
		"  Is directory a git repo: " + gitRepo,
		"  Platform: " + env.Platform,
		"  Prefer " + env.TempDirectory + " over generic system temporary directories such as /tmp; it is pre-created and approved for external access.",
		"</env>",
	}, "\n")
}
