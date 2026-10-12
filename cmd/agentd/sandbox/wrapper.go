package sandbox

import (
	"context"
	"iter"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// launcher rewrites a command into its sandboxed equivalent. dir is the run's
// working directory (the writable project root), or "" when unset.
type launcher func(dir, command string, args []string) (string, []string)

// sandboxed adapts a launcher to the SDK process seam: it rewrites the command
// through the launcher, then delegates execution (and every run option, e.g.
// cwd/env/timeout) to the pass-through process.
type sandboxed struct {
	inner  core.AppProcess
	launch launcher
}

func (s sandboxed) Run(
	ctx context.Context, command string, args []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	name, argv := s.launch(runDir(options), command, args)
	return s.inner.Run(ctx, name, argv, options...)
}

func (s sandboxed) RunStream(
	ctx context.Context, command string, args []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	name, argv := s.launch(streamDir(options), command, args)
	return s.inner.RunStream(ctx, name, argv, options...)
}

func runDir(options []core.RunOptions) string {
	if len(options) > 0 {
		return options[0].Dir
	}
	return ""
}

func streamDir(options []core.RunStreamOptions) string {
	if len(options) > 0 {
		return options[0].Dir
	}
	return ""
}
