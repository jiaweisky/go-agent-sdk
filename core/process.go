package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrTimedOut is the cause carried by an AppProcessError when a run exceeds its
// configured timeout.
var ErrTimedOut = errors.New("Timed out")

type AppProcessError struct {
	Command  string
	ExitCode *int
	Stderr   *string
	Cause    error
}

func (e *AppProcessError) Error() string {
	detail := ""
	if e.Stderr != nil {
		detail = strings.TrimSpace(*e.Stderr)
	} else if e.Cause != nil {
		detail = e.Cause.Error()
	}
	status := ""
	if e.ExitCode != nil {
		status = fmt.Sprintf(" (exit %d)", *e.ExitCode)
	}
	if detail != "" {
		return fmt.Sprintf("Command failed%s: %s: %s", status, e.Command, detail)
	}
	return fmt.Sprintf("Command failed%s: %s", status, e.Command)
}

func (e *AppProcessError) Unwrap() error {
	return e.Cause
}

type RunOptions struct {
	Dir            string
	Env            map[string]string
	ExtendEnv      bool
	Stdin          []byte
	CombineOutput  bool
	MaxOutputBytes *int
	MaxErrorBytes  *int
	Timeout        time.Duration
	Detached       bool
	ForceKillAfter time.Duration
}

type RunStreamOptions struct {
	Dir            string
	Env            map[string]string
	ExtendEnv      bool
	IncludeStderr  bool
	OkExitCodes    []int
	MaxErrorBytes  *int
	Timeout        time.Duration
	Detached       bool
	ForceKillAfter time.Duration
}

type RunResult struct {
	Command         string
	ExitCode        int
	Output          []byte
	Stdout          []byte
	Stderr          []byte
	OutputTruncated bool
	StdoutTruncated bool
	StderrTruncated bool
}

type AppProcess interface {
	Run(ctx context.Context, command string, args []string, options ...RunOptions) (*RunResult, error)
	RunStream(ctx context.Context, command string, args []string, options ...RunStreamOptions) iter.Seq2[string, error]
}

type appProcess struct{}

func NewAppProcess() AppProcess {
	return &appProcess{}
}

func describeCommand(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

func (p *appProcess) command(ctx context.Context, command string, args []string, options RunOptions) *exec.Cmd {
	cmd := exec.CommandContext(ctx, command, args...)
	if options.Dir != "" {
		cmd.Dir = options.Dir
	}
	cmd.Env = os.Environ()
	if !options.ExtendEnv && options.Env != nil {
		cmd.Env = nil
	}
	for k, v := range options.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if options.Stdin != nil {
		cmd.Stdin = bytes.NewReader(options.Stdin)
	}
	applyProcessAttrs(cmd, options.Detached)
	// Mirror the TypeScript spawner: terminate with SIGTERM first, then let
	// WaitDelay force a SIGKILL if the process does not exit in time.
	cmd.Cancel = func() error { return processTerminate(cmd, options.Detached) }
	if options.ForceKillAfter > 0 {
		cmd.WaitDelay = options.ForceKillAfter
	}
	return cmd
}

func collectOutput(r *bytes.Buffer, max *int) ([]byte, bool) {
	data := r.Bytes()
	if max == nil {
		return data, false
	}
	truncated := len(data) > *max
	if truncated {
		data = data[:*max]
	}
	return data, truncated
}

func (p *appProcess) Run(ctx context.Context, command string, args []string, options ...RunOptions) (*RunResult, error) {
	var opts RunOptions
	if len(options) > 0 {
		opts = options[0]
	}
	description := describeCommand(command, args)

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := p.command(ctx, command, args, opts)
	var stdout, stderr, combined bytes.Buffer

	if opts.CombineOutput {
		cmd.Stdout = &combined
		cmd.Stderr = &combined
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, &AppProcessError{Command: description, Cause: ErrTimedOut}
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, &AppProcessError{Command: description, Cause: err}
		}
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	if opts.CombineOutput {
		output, outputTruncated := collectOutput(&combined, opts.MaxOutputBytes)
		return &RunResult{
			Command:         description,
			ExitCode:        exitCode,
			Output:          output,
			Stdout:          []byte{},
			Stderr:          []byte{},
			OutputTruncated: outputTruncated,
		}, nil
	}

	out, stdoutTruncated := collectOutput(&stdout, opts.MaxOutputBytes)
	errOut, stderrTruncated := collectOutput(&stderr, opts.MaxErrorBytes)
	return &RunResult{
		Command:         description,
		ExitCode:        exitCode,
		Stdout:          out,
		Stderr:          errOut,
		StdoutTruncated: stdoutTruncated,
		StderrTruncated: stderrTruncated,
	}, nil
}

func (p *appProcess) RunStream(ctx context.Context, command string, args []string, options ...RunStreamOptions) iter.Seq2[string, error] {
	var opts RunStreamOptions
	if len(options) > 0 {
		opts = options[0]
	}
	description := describeCommand(command, args)

	return func(yield func(string, error) bool) {
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}

		runOpts := RunOptions{
			Dir:            opts.Dir,
			Env:            opts.Env,
			ExtendEnv:      opts.ExtendEnv,
			Detached:       opts.Detached,
			ForceKillAfter: opts.ForceKillAfter,
		}
		cmd := p.command(ctx, command, args, runOpts)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		if opts.IncludeStderr {
			cmd.Stderr = &stdout
		} else {
			cmd.Stderr = &stderr
		}

		err := cmd.Run()
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}

		for _, line := range strings.Split(stdout.String(), "\n") {
			if line == "" {
				continue
			}
			if !yield(line, nil) {
				return
			}
		}

		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				yield("", &AppProcessError{Command: description, Cause: err})
				return
			}
		}
		if len(opts.OkExitCodes) > 0 && !containsInt(opts.OkExitCodes, exitCode) {
			s := stderr.String()
			yield("", &AppProcessError{Command: description, ExitCode: &exitCode, Stderr: &s})
			return
		}
	}
}

func containsInt(items []int, target int) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
