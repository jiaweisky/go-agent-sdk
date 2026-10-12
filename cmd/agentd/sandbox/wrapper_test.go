package sandbox

import (
	"context"
	"iter"
	"reflect"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// recordProcess is a core.AppProcess that records the rewritten invocation and
// replays canned lines, so the sandboxed adapter can be exercised without any OS
// sandbox installed.
type recordProcess struct {
	name  string
	args  []string
	dir   string
	lines []string
}

func (r *recordProcess) Run(
	_ context.Context, name string, args []string, options ...core.RunOptions,
) (*core.RunResult, error) {
	r.name, r.args = name, args
	if len(options) > 0 {
		r.dir = options[0].Dir
	}
	return &core.RunResult{Command: name}, nil
}

func (r *recordProcess) RunStream(
	_ context.Context, name string, args []string, options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	r.name, r.args = name, args
	if len(options) > 0 {
		r.dir = options[0].Dir
	}
	return func(yield func(string, error) bool) {
		for _, line := range r.lines {
			if !yield(line, nil) {
				return
			}
		}
	}
}

// prefixLauncher is a test launcher: it prefixes the command's name and argv with
// the working directory, so the test can assert what sandboxed handed through.
func prefixLauncher(dir, command string, args []string) (string, []string) {
	return "sandbox-" + command, append([]string{"wrap", dir}, args...)
}

func TestSandboxedRunRewritesAndPassesOptions(t *testing.T) {
	rec := &recordProcess{}
	process := sandboxed{inner: rec, launch: prefixLauncher}
	if _, err := process.Run(
		context.Background(), "bash", []string{"-c", "echo hi"}, core.RunOptions{Dir: "/work"},
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.name != "sandbox-bash" {
		t.Fatalf("name = %q, want sandbox-bash", rec.name)
	}
	if want := []string{"wrap", "/work", "-c", "echo hi"}; !reflect.DeepEqual(rec.args, want) {
		t.Fatalf("args = %v, want %v", rec.args, want)
	}
	if rec.dir != "/work" {
		t.Fatalf("dir = %q, want /work", rec.dir)
	}
}

func TestSandboxedRunStreamRewritesAndPassesOptions(t *testing.T) {
	rec := &recordProcess{lines: []string{"a", "b"}}
	process := sandboxed{inner: rec, launch: prefixLauncher}
	var got []string
	for line, err := range process.RunStream(
		context.Background(), "ls", []string{"-l"}, core.RunStreamOptions{Dir: "/work"},
	) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		got = append(got, line)
	}
	if rec.name != "sandbox-ls" {
		t.Fatalf("name = %q, want sandbox-ls", rec.name)
	}
	if want := []string{"wrap", "/work", "-l"}; !reflect.DeepEqual(rec.args, want) {
		t.Fatalf("args = %v, want %v", rec.args, want)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("lines = %v, want [a b]", got)
	}
}

func TestSandboxedRunWithoutOptions(t *testing.T) {
	rec := &recordProcess{}
	if _, err := (sandboxed{inner: rec, launch: prefixLauncher}).Run(
		context.Background(), "true", nil,
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.dir != "" {
		t.Fatalf("dir = %q, want empty when no options are given", rec.dir)
	}
}
