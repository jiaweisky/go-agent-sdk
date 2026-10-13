//go:build windows

package sandbox

import (
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
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func init() {
	Register("appcontainer", func() core.AppProcess { return &appContainerProcess{} })
}

// appContainerProcess runs every command inside a Windows AppContainer: a
// low-integrity sandbox with no network and no filesystem access except the run
// directory the command is granted. It is best-effort — the SDK ships no
// guarantee for it; Windows whole-process isolation is a container/Job Object
// deployment concern.
type appContainerProcess struct{}

const (
	// containerName is the AppContainer profile, created on first use.
	containerName = "go-agent-sdk.agentd.sandbox"
	// procThreadAttributeSecurityCapabilities is
	// PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES.
	procThreadAttributeSecurityCapabilities = 0x00020009
	// extendedStartupInfoPresent makes CreateProcess read the attribute list.
	extendedStartupInfoPresent = 0x00080000
)

// securityCapabilities mirrors the Win32 SECURITY_CAPABILITIES struct.
type securityCapabilities struct {
	appContainerSid *windows.SID
	capabilities    *windows.SIDAndAttributes
	capabilityCount uint32
	reserved        uint32
}

// startupInfoEx mirrors STARTUPINFOEX: a StartupInfo immediately followed by the
// attribute-list pointer, so &sie.startupInfo is the STARTUPINFOEX base.
type startupInfoEx struct {
	startupInfo   windows.StartupInfo
	attributeList *windows.ProcThreadAttributeList
}

var (
	moduserenv                                    = windows.NewLazySystemDLL("userenv.dll")
	procCreateAppContainerProfile                 = moduserenv.NewProc("CreateAppContainerProfile")
	procDeriveAppContainerSidFromAppContainerName = moduserenv.NewProc("DeriveAppContainerSidFromAppContainerName")
)

// appContainerSID returns the SID of the sandbox profile, creating it on first
// use (or deriving it when it already exists).
func appContainerSID(name string) (*windows.SID, error) {
	wide, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var sid *windows.SID
	r1, _, _ := procCreateAppContainerProfile.Call(
		uintptr(unsafe.Pointer(wide)), // name
		uintptr(unsafe.Pointer(wide)), // display name
		uintptr(unsafe.Pointer(wide)), // description
		0, 0,                          // no capabilities
		uintptr(unsafe.Pointer(&sid)),
	)
	if r1 == 0 {
		return sid, nil
	}
	// The profile usually already exists; derive its SID instead.
	r1, _, callErr := procDeriveAppContainerSidFromAppContainerName.Call(
		uintptr(unsafe.Pointer(wide)),
		uintptr(unsafe.Pointer(&sid)),
	)
	if r1 != 0 {
		return nil, fmt.Errorf("derive AppContainer %q: %w", name, callErr)
	}
	return sid, nil
}

// grantedDirs remembers directories already granted to the profile, so the
// (comparatively slow) icacls call runs once per directory, not per command.
var grantedDirs sync.Map

// grantAccess gives the sandbox profile modify access to dir, so the wrapped
// command can read and write its run directory (and, by inheritance, children).
func grantAccess(dir string, sid *windows.SID) error {
	if dir == "" {
		return nil
	}
	if _, ok := grantedDirs.Load(dir); ok {
		return nil
	}
	out, err := exec.Command("icacls", dir, "/grant", "*"+sid.String()+":(OI)(CI)M").CombinedOutput()
	if err != nil {
		return fmt.Errorf("grant %s on %s: %w: %s", sid, dir, err, strings.TrimSpace(string(out)))
	}
	grantedDirs.Store(dir, struct{}{})
	return nil
}

// spawn starts the command inside the AppContainer, returning the process handle
// and the readable ends of its stdout (and stderr, unless merged into stdout).
func spawn(dir, commandLine string, envBlock []uint16, stdin []byte, mergeStderr bool) (windows.Handle, *os.File, *os.File, error) {
	sid, err := appContainerSID(containerName)
	if err != nil {
		return 0, nil, nil, err
	}
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return 0, nil, nil, err
		}
	}
	if err := grantAccess(dir, sid); err != nil {
		return 0, nil, nil, err
	}

	var sa windows.SecurityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.InheritHandle = 1
	newPipe := func() (windows.Handle, windows.Handle, error) {
		var read, write windows.Handle
		if err := windows.CreatePipe(&read, &write, &sa, 0); err != nil {
			return 0, 0, err
		}
		// The parent must not let the read end leak into the child.
		if err := windows.SetHandleInformation(read, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
			windows.CloseHandle(read)
			windows.CloseHandle(write)
			return 0, 0, err
		}
		return read, write, nil
	}
	closeAll := func(handles ...windows.Handle) {
		for _, handle := range handles {
			if handle != 0 {
				windows.CloseHandle(handle)
			}
		}
	}

	outRead, outWrite, err := newPipe()
	if err != nil {
		return 0, nil, nil, err
	}
	stdinRead, stdinWrite, err := newPipe()
	if err != nil {
		closeAll(outRead, outWrite)
		return 0, nil, nil, err
	}
	var errRead, errWrite windows.Handle
	if !mergeStderr {
		if errRead, errWrite, err = newPipe(); err != nil {
			closeAll(outRead, outWrite, stdinRead, stdinWrite)
			return 0, nil, nil, err
		}
	} else {
		errWrite = outWrite
	}

	attributeList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		closeAll(outRead, outWrite, stdinRead, stdinWrite, errRead)
		return 0, nil, nil, err
	}
	capabilities := securityCapabilities{appContainerSid: sid}
	if err := attributeList.Update(
		procThreadAttributeSecurityCapabilities,
		unsafe.Pointer(&capabilities),
		unsafe.Sizeof(capabilities),
	); err != nil {
		attributeList.Delete()
		closeAll(outRead, outWrite, stdinRead, stdinWrite, errRead)
		return 0, nil, nil, err
	}

	var startup startupInfoEx
	startup.startupInfo = windows.StartupInfo{
		Cb:        uint32(unsafe.Sizeof(startup)),
		Flags:     windows.STARTF_USESTDHANDLES,
		StdInput:  stdinRead,
		StdOutput: outWrite,
		StdErr:    errWrite,
	}
	startup.attributeList = attributeList.List()

	commandPtr, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		attributeList.Delete()
		closeAll(outRead, outWrite, stdinRead, stdinWrite, errRead)
		return 0, nil, nil, err
	}
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		attributeList.Delete()
		closeAll(outRead, outWrite, stdinRead, stdinWrite, errRead)
		return 0, nil, nil, err
	}
	var info windows.ProcessInformation
	err = windows.CreateProcess(
		nil, commandPtr, nil, nil, true,
		windows.CREATE_UNICODE_ENVIRONMENT|extendedStartupInfoPresent,
		&envBlock[0], dirPtr, &startup.startupInfo, &info,
	)
	attributeList.Delete()
	windows.CloseHandle(outWrite)
	if !mergeStderr {
		windows.CloseHandle(errWrite)
	}
	windows.CloseHandle(stdinRead)
	if err != nil {
		closeAll(stdinWrite, outRead, errRead)
		return 0, nil, nil, err
	}
	windows.CloseHandle(info.Thread)
	// Feed stdin (when given) and close the write end so the child sees EOF.
	stdinFile := os.NewFile(uintptr(stdinWrite), "stdin")
	if len(stdin) > 0 {
		go func() { _, _ = stdinFile.Write(stdin); _ = stdinFile.Close() }()
	} else {
		_ = stdinFile.Close()
	}
	stderr := (*os.File)(nil)
	if !mergeStderr {
		stderr = os.NewFile(uintptr(errRead), "stderr")
	}
	return info.Process, os.NewFile(uintptr(outRead), "stdout"), stderr, nil
}

// envBlock renders the child environment (base = os.Environ() unless a non-nil
// Env replaces it).
func envBlock(env map[string]string, extend bool) []uint16 {
	var block []uint16
	appendPair := func(pair string) {
		wide, err := syscall.UTF16FromString(pair)
		if err != nil {
			return
		}
		block = append(block, wide...)
	}
	if extend || env == nil {
		for _, pair := range os.Environ() {
			appendPair(pair)
		}
	}
	for key, value := range env {
		appendPair(key + "=" + value)
	}
	return append(block, 0)
}

// wait blocks until the process exits, its context is canceled, or the timeout
// elapses (returning core.ErrTimedOut for the last).
func wait(ctx context.Context, process windows.Handle, timeout time.Duration) (uint32, error) {
	stop := make(chan struct{})
	defer close(stop)
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = windows.TerminateProcess(process, 1)
			case <-stop:
			}
		}()
	}
	millis := uint32(windows.INFINITE)
	if timeout > 0 {
		millis = uint32(timeout / time.Millisecond)
	}
	event, err := windows.WaitForSingleObject(process, millis)
	if err != nil {
		return 0, err
	}
	if event == uint32(windows.WAIT_TIMEOUT) {
		_ = windows.TerminateProcess(process, 1)
		_, _ = windows.WaitForSingleObject(process, windows.INFINITE)
		return 0, core.ErrTimedOut
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
		return 0, err
	}
	return exitCode, nil
}

func (p *appContainerProcess) Run(
	ctx context.Context,
	command string,
	args []string,
	options ...core.RunOptions,
) (*core.RunResult, error) {
	var opts core.RunOptions
	if len(options) > 0 {
		opts = options[0]
	}
	description := util.DescribeCommand(command, args)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	commandLine := windows.ComposeCommandLine(append([]string{command}, args...))
	process, stdout, stderr, err := spawn(opts.Dir, commandLine, envBlock(opts.Env, opts.ExtendEnv), opts.Stdin, opts.CombineOutput)
	if err != nil {
		return nil, &core.AppProcessError{Command: description, Cause: err}
	}
	defer windows.CloseHandle(process)

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = io.Copy(&stdoutBuf, stdout) }()
	if stderr != nil {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = io.Copy(&stderrBuf, stderr) }()
	}
	exitCode, waitErr := wait(ctx, process, opts.Timeout)
	wg.Wait()
	_ = stdout.Close()
	if stderr != nil {
		_ = stderr.Close()
	}
	if errors.Is(waitErr, core.ErrTimedOut) {
		return nil, &core.AppProcessError{Command: description, Cause: core.ErrTimedOut}
	}
	if waitErr != nil {
		return nil, &core.AppProcessError{Command: description, Cause: waitErr}
	}
	if opts.CombineOutput {
		merged := append(stdoutBuf.Bytes(), stderrBuf.Bytes()...)
		output, truncated := util.Truncate(merged, opts.MaxOutputBytes)
		return &core.RunResult{
			Command:         description,
			ExitCode:        int(exitCode),
			Output:          output,
			Stdout:          []byte{},
			Stderr:          []byte{},
			OutputTruncated: truncated,
		}, nil
	}
	out, stdoutTruncated := util.Truncate(stdoutBuf.Bytes(), opts.MaxOutputBytes)
	errOut, stderrTruncated := util.Truncate(stderrBuf.Bytes(), opts.MaxErrorBytes)
	return &core.RunResult{
		Command:         description,
		ExitCode:        int(exitCode),
		Stdout:          out,
		Stderr:          errOut,
		StdoutTruncated: stdoutTruncated,
		StderrTruncated: stderrTruncated,
	}, nil
}

func (p *appContainerProcess) RunStream(
	ctx context.Context,
	command string,
	args []string,
	options ...core.RunStreamOptions,
) iter.Seq2[string, error] {
	var opts core.RunStreamOptions
	if len(options) > 0 {
		opts = options[0]
	}
	description := util.DescribeCommand(command, args)
	return func(yield func(string, error) bool) {
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}
		commandLine := windows.ComposeCommandLine(append([]string{command}, args...))
		process, stdout, stderr, err := spawn(opts.Dir, commandLine, envBlock(opts.Env, opts.ExtendEnv), nil, opts.IncludeStderr)
		if err != nil {
			yield("", &core.AppProcessError{Command: description, Cause: err})
			return
		}
		defer windows.CloseHandle(process)

		if stderr != nil {
			go func() { _, _ = io.Copy(io.Discard, stderr); _ = stderr.Close() }()
		}
		stopped, readFailure := util.StreamReadLines(stdout, yield)
		_ = stdout.Close()
		exitCode, waitErr := wait(ctx, process, opts.Timeout)
		if stopped {
			return
		}
		if errors.Is(waitErr, core.ErrTimedOut) {
			yield("", &core.AppProcessError{Command: description, Cause: core.ErrTimedOut})
			return
		}
		if readFailure != nil {
			yield("", &core.AppProcessError{Command: description, Cause: readFailure})
			return
		}
		if waitErr != nil {
			yield("", &core.AppProcessError{Command: description, Cause: waitErr})
			return
		}
		if len(opts.OkExitCodes) > 0 && !slices.Contains(opts.OkExitCodes, int(exitCode)) {
			code := int(exitCode)
			empty := ""
			yield("", &core.AppProcessError{Command: description, ExitCode: &code, Stderr: &empty})
		}
	}
}
