package sandbox

import (
	"context"
	"iter"
	"os"
	"reflect"
	"strings"
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
func prefixLauncher(dir string, _ Policy, command string, args []string) (string, []string) {
	return "sandbox-" + command, append([]string{"wrap", dir}, args...)
}

func TestSandboxedRunRewritesAndPassesOptions(t *testing.T) {
	rec := &recordProcess{}
	process := sandboxed{inner: rec, launch: prefixLauncher, policy: Policy{}}
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
	process := sandboxed{inner: rec, launch: prefixLauncher, policy: Policy{}}
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

// tempProcess records the child environment so the per-run temp can be checked.
type tempProcess struct {
	env  map[string]string
	temp string
}

func (p *tempProcess) Run(_ context.Context, _ string, _ []string, options ...core.RunOptions) (*core.RunResult, error) {
	if len(options) > 0 {
		p.env = options[0].Env
		p.temp = p.env["TMPDIR"]
	}
	return &core.RunResult{}, nil
}

func (p *tempProcess) RunStream(
	context.Context, string, []string, ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	return func(func(string, error) bool) {}
}

// TestSandboxedUsesPrivateTemp checks each run gets a temp under TempRoot, the
// child env points at it, and the directory is removed afterwards.
func TestSandboxedUsesPrivateTemp(t *testing.T) {
	root := t.TempDir()
	rec := &tempProcess{}
	process := sandboxed{inner: rec, launch: prefixLauncher, policy: Policy{
		TempRoot: root, ReadMode: FSModeAllow, WriteMode: FSModeAllow,
	}}
	if _, err := process.Run(context.Background(), "true", nil, core.RunOptions{Dir: root}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.temp == "" || !strings.HasPrefix(rec.temp, root) {
		t.Fatalf("TMPDIR = %q, want under %q", rec.temp, root)
	}
	if _, err := os.Stat(rec.temp); !os.IsNotExist(err) {
		t.Fatalf("the per-run temp was not cleaned up: %v", err)
	}
}

// resultProcess returns a canned result, so the command-side violation scan can
// be exercised without an OS sandbox.
type resultProcess struct {
	result core.RunResult
}

func (p resultProcess) Run(context.Context, string, []string, ...core.RunOptions) (*core.RunResult, error) {
	result := p.result
	return &result, nil
}

func (p resultProcess) RunStream(
	context.Context, string, []string, ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	return func(func(string, error) bool) {}
}

func TestReportingProcessReportsCommandViolation(t *testing.T) {
	var got core.PolicyViolation
	process := reportingProcess{inner: resultProcess{result: core.RunResult{
		ExitCode: 1,
		Stderr:   []byte("cat: /private/tmp/secret: Operation not permitted"),
	}}}
	if _, err := process.Run(context.Background(), "cat", []string{"/private/tmp/secret"}, core.RunOptions{
		OnViolation: func(v core.PolicyViolation) { got = v },
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Surface != "command" || got.Certainty != "suspected" {
		t.Fatalf("violation = %+v, want a suspected command violation", got)
	}
	if got.Path != "/private/tmp/secret" {
		t.Fatalf("path = %q", got.Path)
	}
}

// TestSandboxedNormalFailureIsNotViolation guards against reporting an ordinary
// command failure as a sandbox denial.
func TestReportingProcessNormalFailureIsNotViolation(t *testing.T) {
	reported := false
	process := reportingProcess{inner: resultProcess{result: core.RunResult{ExitCode: 1, Stderr: []byte("boom")}}}
	if _, err := process.Run(context.Background(), "false", nil, core.RunOptions{
		OnViolation: func(core.PolicyViolation) { reported = true },
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if reported {
		t.Fatal("an ordinary failure must not be reported as a sandbox denial")
	}
}

// TestReportingProcessPrecise parses a syscall-level denial, marks it enforced
// and strips the strace line from the tool output.
func TestReportingProcessPrecise(t *testing.T) {
	var got core.PolicyViolation
	process := reportingProcess{
		inner: resultProcess{result: core.RunResult{
			ExitCode: 1,
			Stderr:   []byte("openat(AT_FDCWD, \"/etc/passwd\", O_RDONLY) = -1 EACCES (Permission denied)\nboom"),
		}},
		precise: true,
	}
	result, err := process.Run(context.Background(), "cat", []string{"/etc/passwd"}, core.RunOptions{
		OnViolation: func(v core.PolicyViolation) { got = v },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Certainty != "enforced" || got.Action != "file" || got.Path != "/etc/passwd" {
		t.Fatalf("violation = %+v", got)
	}
	if strings.Contains(string(result.Stderr), "openat(") {
		t.Fatalf("strace output leaked into the tool output: %q", result.Stderr)
	}
}

// TestReportingProcessPreciseNetwork checks a network denial is recognized (it
// is ENETUNREACH, not EACCES).
func TestReportingProcessPreciseNetwork(t *testing.T) {
	var got core.PolicyViolation
	process := reportingProcess{
		inner: resultProcess{result: core.RunResult{
			ExitCode: 1,
			Stderr:   []byte(`connect(3, {sa_family=AF_INET, sin_port=htons(80), sin_addr=inet_addr("1.2.3.4")}, 16) = -1 ENETUNREACH (Network is unreachable)`),
		}},
		precise: true,
	}
	if _, err := process.Run(context.Background(), "curl", nil, core.RunOptions{
		OnViolation: func(v core.PolicyViolation) { got = v },
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Action != "network" || got.Certainty != "enforced" || got.Host != "1.2.3.4" {
		t.Fatalf("violation = %+v", got)
	}
}

// TestReportingProcessPreciseStripsSuccessfulSyscalls guards against leaking
// successful strace lines into the tool output (or reporting them).
func TestReportingProcessPreciseStripsSuccessfulSyscalls(t *testing.T) {
	reported := false
	process := reportingProcess{
		inner: resultProcess{result: core.RunResult{
			ExitCode: 0,
			Stderr:   []byte("openat(AT_FDCWD, \"/ok\", O_RDONLY) = 3\nhello"),
		}},
		precise: true,
	}
	result, err := process.Run(context.Background(), "cat", nil, core.RunOptions{
		OnViolation: func(core.PolicyViolation) { reported = true },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reported {
		t.Fatal("a successful syscall must not be reported")
	}
	if strings.Contains(string(result.Stderr), "openat(") {
		t.Fatalf("successful strace output leaked: %q", result.Stderr)
	}
	if !strings.Contains(string(result.Stderr), "hello") {
		t.Fatalf("the command's own stderr was lost: %q", result.Stderr)
	}
}

func TestReportingProcessReportToModel(t *testing.T) {
	process := reportingProcess{
		inner: resultProcess{result: core.RunResult{
			ExitCode: 1, Output: []byte("cat: /tmp/x: Operation not permitted"),
		}},
		reportToModel: true,
	}
	result, err := process.Run(context.Background(), "cat", nil, core.RunOptions{
		CombineOutput: true, OnViolation: func(core.PolicyViolation) {},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(string(result.Output), "[sandbox] blocked") {
		t.Fatalf("the denial note is missing from the output: %q", result.Output)
	}
}

// TestReportingProcessReportToModelUsesStderr keeps the note out of stdout,
// which tools such as ripgrep parse.
func TestReportingProcessReportToModelUsesStderr(t *testing.T) {
	process := reportingProcess{
		inner: resultProcess{result: core.RunResult{
			ExitCode: 1,
			Output:   []byte("match\n"),
			Stderr:   []byte("cat: /tmp/x: Operation not permitted"),
		}},
		reportToModel: true,
	}
	result, err := process.Run(context.Background(), "cat", nil, core.RunOptions{
		OnViolation: func(core.PolicyViolation) {},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(string(result.Output), "[sandbox]") {
		t.Fatalf("the note polluted stdout: %q", result.Output)
	}
	if !strings.Contains(string(result.Stderr), "[sandbox]") {
		t.Fatalf("the note is missing from stderr: %q", result.Stderr)
	}
}

func TestSandboxedRunWithoutOptions(t *testing.T) {
	rec := &recordProcess{}
	if _, err := (sandboxed{inner: rec, launch: prefixLauncher, policy: Policy{}}).Run(
		context.Background(), "true", nil,
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.dir != "" {
		t.Fatalf("dir = %q, want empty when no options are given", rec.dir)
	}
}
