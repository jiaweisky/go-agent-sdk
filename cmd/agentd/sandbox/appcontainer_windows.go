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
	"path/filepath"
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
	Register("appcontainer", func(policy Policy) core.AppProcess {
		return &appContainerProcess{policy: policy}
	})
	RegisterProcess("appcontainer", execInAppContainer)
}

// appContainerProcess runs every command inside a Windows AppContainer: a
// low-integrity sandbox with no network and no filesystem access except the
// paths the policy grants. It is best-effort — the SDK ships no guarantee for
// it; Windows whole-process isolation is a container/Job Object deployment
// concern.
type appContainerProcess struct {
	policy Policy
}

const (
	// containerName is the AppContainer profile, created on first use.
	containerName = "go-agent-sdk.agentd.sandbox"
	// procThreadAttributeSecurityCapabilities is
	// PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES.
	procThreadAttributeSecurityCapabilities = 0x00020009
	// procThreadAttributeHandleList is PROC_THREAD_ATTRIBUTE_HANDLE_LIST.
	procThreadAttributeHandleList = 0x00020002
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

// grantedReads remembers directories already granted read-only access.
var grantedReads sync.Map

// grantedTraverse remembers directories already granted traverse-only access.
var grantedTraverse sync.Map

// grantTraverse lets the profile walk to a child directory without listing or
// reading its siblings.
func grantTraverse(path string, sid *windows.SID) error {
	if path == "" {
		return nil
	}
	if _, ok := grantedTraverse.Load(path); ok {
		return nil
	}
	if err := icacls(path, sid, "(X)"); err != nil {
		return err
	}
	grantedTraverse.Store(path, struct{}{})
	return nil
}

// grantTempTraversal grants traverse access to the per-run temp's base and its
// parent, so the profile can reach the run directory under the state root.
func grantTempTraversal(policy Policy, sid *windows.SID) error {
	if policy.TempRoot == "" {
		return nil
	}
	for _, dir := range []string{policy.TempRoot, filepath.Dir(policy.TempRoot)} {
		if err := grantTraverse(dir, sid); err != nil {
			return err
		}
	}
	return nil
}

// grantRead gives the sandbox profile read and execute access to dir, for paths
// the policy allows beyond the run directory.
func grantRead(dir string, sid *windows.SID) error {
	if dir == "" {
		return nil
	}
	if _, ok := grantedReads.Load(dir); ok {
		return nil
	}
	if err := icacls(dir, sid, "(OI)(CI)RX"); err != nil {
		return err
	}
	grantedReads.Store(dir, struct{}{})
	return nil
}

// grantAccess gives the sandbox profile modify access to dir, so the wrapped
// command can read and write its run directory (and, by inheritance, children).
func grantAccess(dir string, sid *windows.SID) error {
	if dir == "" {
		return nil
	}
	if _, ok := grantedDirs.Load(dir); ok {
		return nil
	}
	if err := icacls(dir, sid, "(OI)(CI)M"); err != nil {
		return err
	}
	grantedDirs.Store(dir, struct{}{})
	return nil
}

// icacls grants the profile SID the given rights mask on path.
func icacls(path string, sid *windows.SID, rights string) error {
	out, err := exec.Command("icacls", path, "/grant", "*"+sid.String()+":"+rights).CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// execInAppContainer re-launches agentd inside the AppContainer profile with the
// internetClientServer capability (so it can serve listeners and call the
// model), granting write access to roots and read/execute to the agentd binary,
// then waits and exits with the child's status. Best-effort: an AppContainer
// blocks loopback by default, so it also attempts a loopback exemption for the
// profile (the host may need to configure one itself).
func execInAppContainer(roots []string, policy Policy, command string, args []string) error {
	sid, err := appContainerSID(containerName)
	if err != nil {
		return err
	}
	for _, root := range append(roots, policy.WriteRoots()...) {
		if err := grantAccess(root, sid); err != nil {
			return err
		}
	}
	for _, root := range policy.AllowRead {
		if err := grantRead(root, sid); err != nil {
			return err
		}
	}
	for _, socket := range policy.SocketPaths() {
		if err := grantAccess(socket, sid); err != nil {
			return err
		}
	}
	if err := grantTempTraversal(policy, sid); err != nil {
		return err
	}
	// The binary itself must be readable and executable by the profile.
	if err := icacls(command, sid, "(RX)"); err != nil {
		return err
	}
	exemptLoopback(sid)

	security, err := networkSecurity(sid, true)
	if err != nil {
		return err
	}

	// The re-executed process keeps this process's std handles. A handle the
	// host left non-inheritable (a redirected output file, say) would dangle in
	// the child — with bInheritHandles=TRUE the value is copied, the handle is
	// not — and a dangling std handle inside an AppContainer crashes the child
	// during initialization (the same failure the per-command spawn() guards
	// against). Make every std handle inheritable and restrict inheritance to
	// exactly those handles. HANDLE_LIST entries must be inheritable and must
	// not repeat.
	inherited := make([]windows.Handle, 0, 3)
	for _, id := range []uint32{
		windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE,
	} {
		handle := stdHandle(id)
		if handle == 0 {
			continue
		}
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return err
		}
		if !slices.Contains(inherited, handle) {
			inherited = append(inherited, handle)
		}
	}

	attributeList, err := newChildAttributeList(security, inherited)
	if err != nil {
		return err
	}
	defer attributeList.Delete()

	var startup startupInfoEx
	startup.startupInfo = windows.StartupInfo{
		Cb:        uint32(unsafe.Sizeof(startup)),
		Flags:     windows.STARTF_USESTDHANDLES,
		StdInput:  stdHandle(windows.STD_INPUT_HANDLE),
		StdOutput: stdHandle(windows.STD_OUTPUT_HANDLE),
		StdErr:    stdHandle(windows.STD_ERROR_HANDLE),
	}
	startup.attributeList = attributeList.List()

	commandPtr, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{command}, args...)))
	if err != nil {
		return err
	}
	var info windows.ProcessInformation
	if err := windows.CreateProcess(
		nil, commandPtr, nil, nil, true,
		windows.CREATE_UNICODE_ENVIRONMENT|extendedStartupInfoPresent,
		nil, nil, &startup.startupInfo, &info,
	); err != nil {
		return err
	}
	windows.CloseHandle(info.Thread)
	_, _ = windows.WaitForSingleObject(info.Process, windows.INFINITE)
	var exitCode uint32
	_ = windows.GetExitCodeProcess(info.Process, &exitCode)
	windows.CloseHandle(info.Process)
	os.Exit(int(exitCode))
	return nil
}

// stdHandle returns a standard handle, or 0 (which makes the child inherit it).
func stdHandle(id uint32) windows.Handle {
	handle, err := windows.GetStdHandle(id)
	if err != nil {
		return 0
	}
	return handle
}

// exemptLoopback best-effort exempts the profile SID from the AppContainer
// loopback block so the bound listener stays reachable. A failure is logged, not
// fatal: the host may configure the exemption itself.
func exemptLoopback(sid *windows.SID) {
	if out, err := exec.Command("CheckNetIsolation.exe", "LoopbackExempt", "-a", "-p="+sid.String()).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "appcontainer: loopback exemption failed (%v): %s\n", err, strings.TrimSpace(string(out)))
	}
}

// stdioPipes owns the pipe ends for one spawned child. The child receives the
// child* ends (its stdin read end and stdout write end, plus a stderr write
// end unless stderr is merged into stdout); the parent keeps the ends it needs
// to feed stdin and read output.
type stdioPipes struct {
	childStdin       windows.Handle
	childStdout      windows.Handle
	childStderr      windows.Handle // same as childStdout when merged
	parentStdinWrite windows.Handle
	parentStdoutRead windows.Handle
	parentStderrRead windows.Handle // 0 when merged
}

// newStdioPipes creates the pipes. On failure every end created so far is
// closed, so the caller holds nothing.
func newStdioPipes(mergeStderr bool) (*stdioPipes, error) {
	pipe := func() (read, write windows.Handle, err error) {
		var sa windows.SecurityAttributes
		sa.Length = uint32(unsafe.Sizeof(sa))
		sa.InheritHandle = 1
		if err = windows.CreatePipe(&read, &write, &sa, 0); err != nil {
			return 0, 0, err
		}
		// The parent must not let a read end leak into the child.
		if err = windows.SetHandleInformation(read, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
			windows.CloseHandle(read)
			windows.CloseHandle(write)
			return 0, 0, err
		}
		return read, write, nil
	}

	p := &stdioPipes{}
	fail := func(err error) (*stdioPipes, error) {
		p.closeAll()
		return nil, err
	}
	var err error
	if p.parentStdoutRead, p.childStdout, err = pipe(); err != nil {
		return fail(err)
	}
	if p.childStdin, p.parentStdinWrite, err = pipe(); err != nil {
		return fail(err)
	}
	// Unlike the other read ends, the stdin read end IS handed to the child,
	// so it must stay inheritable.
	if err := windows.SetHandleInformation(p.childStdin, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return fail(err)
	}
	if mergeStderr {
		p.childStderr = p.childStdout
	} else if p.parentStderrRead, p.childStderr, err = pipe(); err != nil {
		return fail(err)
	}
	return p, nil
}

// childHandles lists the ends the child is started with, as
// PROC_THREAD_ATTRIBUTE_HANDLE_LIST requires them: each handle once, all
// inheritable.
func (p *stdioPipes) childHandles() []windows.Handle {
	handles := []windows.Handle{p.childStdin, p.childStdout}
	if p.childStderr != p.childStdout {
		handles = append(handles, p.childStderr)
	}
	return handles
}

// closeChildEnds releases the parent's copies of the ends the child received.
// Call it once the child is running, so the child sees EOF when the parent
// stops writing.
func (p *stdioPipes) closeChildEnds() {
	for _, handle := range p.childHandles() {
		windows.CloseHandle(handle)
	}
}

// closeAll releases every end still held by the parent.
func (p *stdioPipes) closeAll() {
	all := []windows.Handle{
		p.childStdin, p.childStdout, p.parentStdinWrite, p.parentStdoutRead,
	}
	if p.childStderr != p.childStdout {
		all = append(all, p.childStderr, p.parentStderrRead)
	}
	for _, handle := range all {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
	}
}

// networkSecurity builds the AppContainer security capabilities. allowNetwork
// adds internetClientServer; without it the per-command container has no network
// capability.
func networkSecurity(sid *windows.SID, allowNetwork bool) (securityCapabilities, error) {
	security := securityCapabilities{appContainerSid: sid}
	if !allowNetwork {
		return security, nil
	}
	capability, err := windows.CreateWellKnownSid(windows.WinCapabilityInternetClientServerSid)
	if err != nil {
		return security, err
	}
	capabilities := []windows.SIDAndAttributes{{Sid: capability, Attributes: windows.SE_GROUP_ENABLED}}
	security.capabilities = &capabilities[0]
	security.capabilityCount = uint32(len(capabilities))
	return security, nil
}

// newChildAttributeList builds the process attribute list a spawned child is
// started with: the AppContainer security capabilities plus, when handles are
// given, an explicit handle list. The caller must Delete the returned list.
func newChildAttributeList(
	security securityCapabilities,
	inherited []windows.Handle,
) (*windows.ProcThreadAttributeListContainer, error) {
	attributeCount := uint32(1)
	if len(inherited) > 0 {
		attributeCount = 2
	}
	attributeList, err := windows.NewProcThreadAttributeList(attributeCount)
	if err != nil {
		return nil, err
	}
	if err := attributeList.Update(
		procThreadAttributeSecurityCapabilities,
		unsafe.Pointer(&security),
		unsafe.Sizeof(security),
	); err != nil {
		attributeList.Delete()
		return nil, err
	}
	if len(inherited) > 0 {
		if err := attributeList.Update(
			procThreadAttributeHandleList,
			unsafe.Pointer(&inherited[0]),
			unsafe.Sizeof(inherited[0])*uintptr(len(inherited)),
		); err != nil {
			attributeList.Delete()
			return nil, err
		}
	}
	return attributeList, nil
}

// spawn starts the command inside the AppContainer, returning the process
// handle and the readable ends of its stdout (and stderr, unless merged into
// stdout). On error nothing survives: every pipe end is closed and the returned
// handles and files are invalid.
func spawn(policy Policy, dir, commandLine string, envBlock []uint16, stdin []byte, mergeStderr bool) (process windows.Handle, stdout, stderr *os.File, err error) {
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
	for _, root := range policy.WriteRoots() {
		if err := grantAccess(root, sid); err != nil {
			return 0, nil, nil, err
		}
	}
	for _, root := range policy.AllowRead {
		if err := grantRead(root, sid); err != nil {
			return 0, nil, nil, err
		}
	}
	for _, socket := range policy.SocketPaths() {
		if err := grantAccess(socket, sid); err != nil {
			return 0, nil, nil, err
		}
	}
	if err := grantTempTraversal(policy, sid); err != nil {
		return 0, nil, nil, err
	}

	pipes, err := newStdioPipes(mergeStderr)
	if err != nil {
		return 0, nil, nil, err
	}
	// On success the ends are handed on (output files, the stdin writer) or
	// released via closeChildEnds; on any failure nothing may survive.
	defer func() {
		if err != nil {
			pipes.closeAll()
		}
	}()

	allowNetwork := policy.NetworkMode == NetworkOff
	security, err := networkSecurity(sid, allowNetwork)
	if err != nil {
		return 0, nil, nil, err
	}
	if allowNetwork {
		exemptLoopback(sid)
	}
	attributeList, err := newChildAttributeList(security, pipes.childHandles())
	if err != nil {
		return 0, nil, nil, err
	}
	defer attributeList.Delete()

	// Inside an AppContainer, CreateProcess with bInheritHandles=TRUE corrupts
	// child initialization (STATUS_DLL_INIT_FAILED / access violation) when any
	// inheritable handle is copied. The explicit handle list restricts
	// inheritance to exactly the std pipe handles, which avoids that.
	startup := startupInfoEx{
		startupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(startupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  pipes.childStdin,
			StdOutput: pipes.childStdout,
			StdErr:    pipes.childStderr,
		},
		attributeList: attributeList.List(),
	}
	commandPtr, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return 0, nil, nil, err
	}
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, nil, nil, err
	}
	var info windows.ProcessInformation
	if err = windows.CreateProcess(
		nil, commandPtr, nil, nil, true,
		windows.CREATE_UNICODE_ENVIRONMENT|extendedStartupInfoPresent,
		&envBlock[0], dirPtr, &startup.startupInfo, &info,
	); err != nil {
		return 0, nil, nil, err
	}
	windows.CloseHandle(info.Thread)
	pipes.closeChildEnds()

	// Feed stdin (when given) and close the write end so the child sees EOF.
	stdinFile := os.NewFile(uintptr(pipes.parentStdinWrite), "stdin")
	if len(stdin) > 0 {
		go func() { _, _ = stdinFile.Write(stdin); _ = stdinFile.Close() }()
	} else {
		_ = stdinFile.Close()
	}
	if !mergeStderr {
		stderr = os.NewFile(uintptr(pipes.parentStderrRead), "stderr")
	}
	return info.Process, os.NewFile(uintptr(pipes.parentStdoutRead), "stdout"), stderr, nil
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
	policy, cleanup, err := withPrivateTemp(p.policy)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	options = withTempEnv(options, policy)
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
	process, stdout, stderr, err := spawn(policy, opts.Dir, commandLine, envBlock(opts.Env, opts.ExtendEnv), opts.Stdin, opts.CombineOutput)
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
	description := util.DescribeCommand(command, args)
	return func(yield func(string, error) bool) {
		policy, cleanup, err := withPrivateTemp(p.policy)
		if err != nil {
			yield("", &core.AppProcessError{Command: description, Cause: err})
			return
		}
		defer cleanup()
		options = withStreamTempEnv(options, policy)
		var opts core.RunStreamOptions
		if len(options) > 0 {
			opts = options[0]
		}
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}
		commandLine := windows.ComposeCommandLine(append([]string{command}, args...))
		process, stdout, stderr, err := spawn(policy, opts.Dir, commandLine, envBlock(opts.Env, opts.ExtendEnv), nil, opts.IncludeStderr)
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
