package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// ErrTimedOut is the cause carried by an AppProcessError when a run exceeds its
// configured timeout.
var ErrTimedOut = errors.New("Timed out")

// streamTerminateGrace bounds how long a streamed process may keep running
// after its stream is abandoned or cancelled before it is killed.
const streamTerminateGrace = 2 * time.Second

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
	// RunStream runs a command and yields its stdout lines as they are
	// produced. When IncludeStderr is set, stderr is merged into the stream.
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
	// Terminate with SIGTERM first, then let WaitDelay force a SIGKILL if the
	// process does not exit in time.
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

func (p *appProcess) Run(
	ctx context.Context,
	command string,
	args []string,
	options ...RunOptions,
) (*RunResult, error) {
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

func (p *appProcess) RunStream(
	ctx context.Context,
	command string,
	args []string,
	options ...RunStreamOptions,
) iter.Seq2[string, error] {
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
		runCtx, stop := context.WithCancel(ctx)
		defer stop()

		runOpts := RunOptions{
			Dir:            opts.Dir,
			Env:            opts.Env,
			ExtendEnv:      opts.ExtendEnv,
			Detached:       opts.Detached,
			ForceKillAfter: opts.ForceKillAfter,
		}
		cmd := p.command(runCtx, command, args, runOpts)
		if cmd.WaitDelay == 0 {
			cmd.WaitDelay = streamTerminateGrace
		}

		// Read stdout through a pipe we own so each line is yielded as it is
		// produced rather than after the process exits.
		read, write, err := os.Pipe()
		if err != nil {
			yield("", &AppProcessError{Command: description, Cause: err})
			return
		}
		stderr := newBoundedBuffer(opts.MaxErrorBytes)
		cmd.Stdout = write
		if opts.IncludeStderr {
			cmd.Stderr = write
		} else {
			cmd.Stderr = stderr
		}

		if err := cmd.Start(); err != nil {
			read.Close()
			write.Close()
			yield("", &AppProcessError{Command: description, Cause: err})
			return
		}
		// The parent must release the write end, or the read never reaches EOF.
		write.Close()

		stopped, readFailure := streamReadLines(read, yield)
		read.Close()

		if stopped {
			// stop triggers SIGTERM through cmd.Cancel; WaitDelay then kills a
			// process that ignores it, so an abandoned stream cannot hang.
			stop()
			_ = cmd.Wait()
			return
		}

		waitErr := cmd.Wait()
		if ctx.Err() == context.DeadlineExceeded {
			yield("", &AppProcessError{Command: description, Cause: ErrTimedOut})
			return
		}
		if readFailure != nil {
			yield("", &AppProcessError{Command: description, Cause: readFailure})
			return
		}
		if err := streamExitError(cmd, waitErr, stderr.String(), opts.OkExitCodes, description); err != nil {
			yield("", err)
		}
	}
}

// streamReadLines yields each stdout line until EOF, reporting whether the
// consumer stopped early and any non-EOF read failure.
func streamReadLines(read *os.File, yield func(string, error) bool) (bool, error) {
	var readFailure error
	reader := bufio.NewReader(read)
	for {
		line, readErr := reader.ReadString('\n')
		if line = strings.TrimSuffix(line, "\n"); line != "" {
			if !yield(line, nil) {
				return true, nil
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				readFailure = readErr
			}
			return false, readFailure
		}
	}
}

// streamExitError interprets the wait result against the allowed exit codes.
func streamExitError(cmd *exec.Cmd, waitErr error, stderr string, okExitCodes []int, description string) error {
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return &AppProcessError{Command: description, Cause: waitErr}
		}
	}
	if len(okExitCodes) > 0 && !slices.Contains(okExitCodes, exitCode) {
		return &AppProcessError{Command: description, ExitCode: &exitCode, Stderr: &stderr}
	}
	return nil
}

// boundedBuffer accumulates at most limit bytes. A negative limit is unbounded.
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func newBoundedBuffer(max *int) *boundedBuffer {
	limit := -1
	if max != nil {
		limit = *max
	}
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.limit < 0 {
		return b.buf.Write(p)
	}
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.buf.Write(p)
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string { return b.buf.String() }
