//go:build darwin

package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func init() {
	Register("seatbelt", func(policy Policy) core.AppProcess {
		return newSandboxed(seatbeltLaunch, policy)
	})
	RegisterProcess("seatbelt", seatbeltProcess)
}

// seatbeltLaunch wraps a command in the macOS seatbelt sandbox (`sandbox-exec`)
// under the resolved policy. Best-effort: the SDK ships no guarantee for it;
// `sandbox-exec` is deprecated by Apple.
func seatbeltLaunch(dir string, policy Policy, command string, args []string) (string, []string) {
	argv := []string{"-p", seatbeltCommandProfile(dir, policy), command}
	return "sandbox-exec", append(argv, args...)
}

// seatbeltProcess wraps the whole process in seatbelt under the resolved policy.
// The network stays up (the process serves listeners and calls the model) and
// process introspection is not denied (agentd manages its own children).
func seatbeltProcess(roots []string, policy Policy, command string, args []string) error {
	return execReplace("sandbox-exec", seatbeltProcessArgs(roots, policy, command, args))
}

// seatbeltProcessArgs is the sandbox-exec argument vector (excluding the program
// name).
func seatbeltProcessArgs(roots []string, policy Policy, command string, args []string) []string {
	return append([]string{"-p", seatbeltProcessProfile(roots, policy), command}, args...)
}

// seatbeltCommandProfile is the per-command profile: it adds the network denial
// (when the policy says no egress) and the process-introspection denial.
func seatbeltCommandProfile(dir string, policy Policy) string {
	return writeSeatbeltProfile([]string{dir}, policy, true)
}

// seatbeltProcessProfile is the whole-process profile; it keeps the network and
// process introspection up.
func seatbeltProcessProfile(roots []string, policy Policy) string {
	var clauses []string
	clauses = append(clauses, roots...)
	clauses = append(clauses, policy.WorkingDirectory)
	return writeSeatbeltProfile(clauses, policy, false)
}

// writeSeatbeltProfile renders the sandbox-exec profile in precedence order
// (later rules win): default allow, then network, then writes, then reads,
// then process introspection.
func writeSeatbeltProfile(dirs []string, policy Policy, perCommand bool) string {
	var profile strings.Builder
	profile.WriteString("(version 1)(allow default)")
	if perCommand && policy.NetworkMode == NetworkNoEgress {
		profile.WriteString(seatbeltNetworkDeny())
	}
	profile.WriteString(seatbeltWriteRules(dirs, policy))
	profile.WriteString(seatbeltReadRules(dirs, policy))
	if perCommand && policy.DenyProcessInfo {
		profile.WriteString(seatbeltProcessInfoDeny())
	}
	return profile.String()
}

// seatbeltNetworkDeny denies remote IP traffic (no egress) while leaving
// AF_UNIX sockets to the filesystem rules. A bare "(deny network*)" would also
// block AF_UNIX, making the socket allowlist ineffective.
func seatbeltNetworkDeny() string {
	return "(deny network* (remote ip))"
}

func seatbeltProcessInfoDeny() string {
	return "(deny process-info*)(deny sysctl-read)"
}

func seatbeltWriteRules(dirs []string, policy Policy) string {
	if policy.WriteMode != FSModeAllow {
		return ""
	}
	roots := dedupe(append(append([]string{}, dirs...), policy.WriteRoots()...))
	roots = append(roots, policy.SocketPaths()...)
	return "(deny file-write*)(allow file-write* " + seatbeltClauses(roots) + ")" +
		seatbeltDenies("file-write*", policy.DeniedWrite())
}

func seatbeltReadRules(dirs []string, policy Policy) string {
	if policy.ReadMode != FSModeAllow {
		// read: off means no read confinement; no credential masks either.
		return ""
	}
	// Deny broad regions (home, /etc), then re-allow the project and any
	// explicitly allowed paths. System run-time paths are never denied.
	roots := append(append([]string{}, dirs...), policy.ReallowReadRoots()...)
	roots = append(roots, policy.SocketPaths()...)
	return seatbeltDenies("file-read*", policy.DenyReadRegions()) +
		"(allow file-read* " + seatbeltClauses(dedupe(roots)) + ")" +
		seatbeltDenies("file-read*", policy.DeniedRead())
}

// seatbeltClauses renders a list of paths as `(subpath "...")` clauses.
func seatbeltClauses(paths []string) string {
	var b strings.Builder
	for _, path := range paths {
		if path == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		fmt.Fprintf(&b, "(subpath %q)", path)
	}
	return b.String()
}

// seatbeltDenies renders deny rules: globs become regexes, literals subpaths.
func seatbeltDenies(kind string, patterns []string) string {
	var b strings.Builder
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if strings.ContainsAny(pattern, "*?[") {
			fmt.Fprintf(&b, "(deny %s (regex %q))", kind, globRegex(pattern))
			continue
		}
		if resolved, err := filepath.EvalSymlinks(pattern); err == nil {
			pattern = resolved
		}
		fmt.Fprintf(&b, "(deny %s (subpath %q))", kind, pattern)
	}
	return b.String()
}
