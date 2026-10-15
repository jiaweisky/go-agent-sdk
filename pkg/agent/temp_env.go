package agent

import (
	"context"
	"iter"
	"os"
	"path/filepath"

	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// agentTempDir is the temporary directory the model is told to prefer and that
// spawned commands inherit. A sandbox denies writes outside the working
// directory and the system temp directory, so it lives under the latter and is
// scoped per instance by a hash of the state root.
func agentTempDir(root string) string {
	return filepath.Join(os.TempDir(), "go-agent-sdk", util.HashFastString(root))
}

// tempEnvProcess injects the SDK state temp directory into every spawned
// command (TMPDIR, plus TEMP/TMP on Windows) so tool subprocesses keep their
// scratch under the state root instead of the system temp directory. It wraps
// the host's process seam, so it applies uniformly to bash, ripgrep and git.
type tempEnvProcess struct {
	inner core.AppProcess
	dir   string
}

// withTempEnv wraps a process seam so every command inherits dir as its temp
// directory. An empty dir, or a nil seam, is a no-op.
func withTempEnv(inner core.AppProcess, dir string) core.AppProcess {
	if dir == "" || inner == nil {
		return inner
	}
	return tempEnvProcess{inner: inner, dir: dir}
}

func (p tempEnvProcess) Run(
	ctx context.Context, command string, args []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	return p.inner.Run(ctx, command, args, withRunTempEnv(options, p.dir)...)
}

func (p tempEnvProcess) RunStream(
	ctx context.Context, command string, args []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	return p.inner.RunStream(ctx, command, args, withStreamTempEnv(options, p.dir)...)
}

func withRunTempEnv(options []core.RunOptions, dir string) []core.RunOptions {
	var opts core.RunOptions
	if len(options) > 0 {
		opts = options[0]
	}
	opts.Env, opts.ExtendEnv = tempEnvVars(opts.Env, opts.ExtendEnv, dir)
	return []core.RunOptions{opts}
}

func withStreamTempEnv(options []core.RunStreamOptions, dir string) []core.RunStreamOptions {
	var opts core.RunStreamOptions
	if len(options) > 0 {
		opts = options[0]
	}
	opts.Env, opts.ExtendEnv = tempEnvVars(opts.Env, opts.ExtendEnv, dir)
	return []core.RunStreamOptions{opts}
}

// tempEnvVars returns env with the temp variables pointed at dir, copying it so
// the caller's map is never mutated. It reports the effective ExtendEnv: an
// unset env means "inherit the base environment", which needs ExtendEnv so PATH
// and friends survive; a caller that set an env keeps its own ExtendEnv.
func tempEnvVars(env map[string]string, extend bool, dir string) (map[string]string, bool) {
	if env == nil {
		extend = true
	}
	merged := make(map[string]string, len(env)+3)
	for key, value := range env {
		merged[key] = value
	}
	// TMPDIR is the POSIX convention; TEMP/TMP are the Windows ones.
	merged["TMPDIR"] = dir
	merged["TEMP"] = dir
	merged["TMP"] = dir
	return merged, extend
}
