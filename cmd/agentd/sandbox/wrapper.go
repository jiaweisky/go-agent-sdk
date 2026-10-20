package sandbox

import (
	"context"
	"iter"
	"os"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// launcher rewrites a command into its sandboxed equivalent. dir is the run's
// working directory (the writable project root), or "" when unset.
type launcher func(dir string, policy Policy, command string, args []string) (string, []string)

// sandboxTempDirMode keeps a per-run temp private to the server user.
const sandboxTempDirMode os.FileMode = 0o700

// sandboxed adapts a launcher to the SDK process seam. Each run gets a private
// temp directory under the policy's TempRoot, so a command cannot reach sockets
// or files another process left in the shared host temp.
type sandboxed struct {
	inner  core.AppProcess
	launch launcher
	policy Policy
}

func newSandboxed(factory launcher, policy Policy) *sandboxed {
	return &sandboxed{inner: core.NewAppProcess(), launch: factory, policy: policy}
}

func (s sandboxed) Run(
	ctx context.Context, command string, args []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	policy, cleanup, err := withPrivateTemp(s.policy)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	options = withTempEnv(options, policy)
	name, argv := s.launch(runDir(options), policy, command, args)
	return s.inner.Run(ctx, name, argv, options...)
}

func (s sandboxed) RunStream(
	ctx context.Context, command string, args []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		policy, cleanup, err := withPrivateTemp(s.policy)
		if err != nil {
			yield("", err)
			return
		}
		defer cleanup()
		options = withStreamTempEnv(options, policy)
		name, argv := s.launch(streamDir(options), policy, command, args)
		for line, err := range s.inner.RunStream(ctx, name, argv, options...) {
			if !yield(line, err) {
				return
			}
		}
	}
}

// withPrivateTemp creates a per-run private temp and returns a policy copy that
// uses it. The copy is local to the run; the shared policy is not mutated.
func withPrivateTemp(policy Policy) (Policy, func(), error) {
	base := policy.TempRoot
	if base == "" {
		base = os.TempDir()
	}
	if err := os.MkdirAll(base, sandboxTempDirMode); err != nil {
		return Policy{}, nil, err
	}
	temp, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return Policy{}, nil, err
	}
	policy.PrivateTemp = temp
	return policy, func() { _ = os.RemoveAll(temp) }, nil
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

// withTempEnv points the child's temp variables at the private directory while
// preserving the caller's environment semantics.
func withTempEnv(options []core.RunOptions, policy Policy) []core.RunOptions {
	out := append([]core.RunOptions{}, options...)
	if len(out) == 0 {
		out = []core.RunOptions{{}}
	}
	opts := out[0]
	opts.Env, opts.ExtendEnv = tempEnv(opts.Env, opts.ExtendEnv, policy)
	out[0] = opts
	return out
}

func withStreamTempEnv(options []core.RunStreamOptions, policy Policy) []core.RunStreamOptions {
	out := append([]core.RunStreamOptions{}, options...)
	if len(out) == 0 {
		out = []core.RunStreamOptions{{}}
	}
	opts := out[0]
	opts.Env, opts.ExtendEnv = tempEnv(opts.Env, opts.ExtendEnv, policy)
	out[0] = opts
	return out
}

// tempEnv merges the private temp variables into the child environment. An
// empty, non-extending environment means "inherit", so it is preserved by
// switching to extend mode before adding the variables. In heuristic violation
// mode the child locale is pinned to C: the denial scan matches English OS
// messages, which a localized locale would replace with unmatchable text.
func tempEnv(env map[string]string, extend bool, policy Policy) (map[string]string, bool) {
	merged := make(map[string]string, len(env)+4)
	for key, value := range env {
		merged[key] = value
	}
	if env == nil && !extend {
		extend = true
	}
	merged["TMPDIR"], merged["TMP"], merged["TEMP"] = policy.PrivateTemp, policy.PrivateTemp, policy.PrivateTemp
	if policy.ViolationMode == ViolationHeuristic {
		merged["LC_ALL"] = "C"
	}
	return merged, extend
}
