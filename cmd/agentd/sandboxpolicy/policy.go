// Package sandboxpolicy translates the sandbox configuration into the policy
// used by the sandbox implementation and the in-process file guards.
package sandboxpolicy

import (
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/sandbox"
)

// ForServer resolves a server's sandbox policy for its working directory. A
// disabled sandbox yields a policy with only the working directory set, so the
// guards become no-ops.
func ForServer(cfg *config.SandboxConfig, workingDir string) sandbox.Policy {
	if !cfg.Enabled() {
		return sandbox.Policy{WorkingDirectory: workingDir}
	}

	return resolve(cfg.SandboxPolicy, workingDir)
}

// ForWrap resolves the whole-process sandbox policy. A disabled wrap yields the
// zero policy.
func ForWrap(cfg *config.WrapConfig) sandbox.Policy {
	if !cfg.Enabled() {
		return sandbox.Policy{}
	}

	return resolve(cfg.SandboxPolicy, "")
}

// profileHasStandardDefaults reports whether the profile starts from the
// standard secure defaults.
func profileHasStandardDefaults(profile config.Profile) bool {
	return profile == config.ProfileDefault || profile == config.ProfileStandard
}

// resolve builds a policy from a profile and the explicit overrides.
func resolve(p config.SandboxPolicy, workingDir string) sandbox.Policy {
	// Callers normally filter disabled configs first; keep this defensive.
	if p.Profile == config.ProfileOff {
		return sandbox.Policy{WorkingDirectory: workingDir}
	}

	policy := basePolicy(p, workingDir)
	applyFilesystemPaths(&policy, p.Filesystem)
	applyNetwork(&policy, p.Network)
	applyUnixSockets(&policy, p.UnixSockets)
	applyProcessInspection(&policy, p.ProcessInspection)
	applyEnv(&policy, p.Env)
	applyViolations(&policy, p.Violations, profileHasStandardDefaults(p.Profile))

	return policy
}

// basePolicy is the profile's starting point: secure defaults for standard,
// no confinement for custom.
func basePolicy(p config.SandboxPolicy, workingDir string) sandbox.Policy {
	var policy sandbox.Policy

	switch {
	case profileHasStandardDefaults(p.Profile):
		policy = sandbox.StandardPolicy(workingDir)
	case p.Profile == config.ProfileCustom:
		policy = sandbox.Policy{
			WorkingDirectory: workingDir,
			NetworkMode:      sandbox.NetworkOff,
			SocketMode:       sandbox.SocketAllowAll,
		}
	default:
		// Unvalidated or unknown profiles get no confinement.
		policy = sandbox.Policy{WorkingDirectory: workingDir}
	}

	policy.ReadMode, policy.WriteMode = p.EffectiveAccess()

	return policy
}

// applyFilesystemPaths applies the explicit path overrides.
func applyFilesystemPaths(policy *sandbox.Policy, filesystem *config.SandboxFilesystem) {
	if filesystem == nil {
		return
	}

	policy.DenyRead = append(policy.DenyRead, filesystem.DenyRead...)
	policy.AllowRead = append(policy.AllowRead, filesystem.AllowRead...)
	policy.DenyWrite = append(policy.DenyWrite, filesystem.DenyWrite...)
	policy.AllowWrite = append(policy.AllowWrite, filesystem.AllowWrite...)
}

// applyNetwork applies the network mode.
func applyNetwork(policy *sandbox.Policy, network *config.SandboxNetwork) {
	if network != nil && network.Mode != "" {
		policy.NetworkMode = network.Mode
	}
}

// applyUnixSockets applies the socket mode and allow-list. The legacy "deny"
// value is normalized to "inherit".
func applyUnixSockets(policy *sandbox.Policy, sockets *config.SandboxUnixSockets) {
	if sockets == nil {
		return
	}

	switch sockets.Mode {
	case sandbox.SocketDeny:
		policy.SocketMode = sandbox.SocketInherit
	case "":
		// Keep the profile default.
	default:
		policy.SocketMode = sockets.Mode
	}

	policy.SocketAllow = append(policy.SocketAllow, sockets.Allow...)
}

// applyProcessInspection applies the process-inspection override.
func applyProcessInspection(policy *sandbox.Policy, mode string) {
	switch mode {
	case "deny":
		policy.DenyProcessInfo = true
	case "allow":
		policy.DenyProcessInfo = false
	}
}

// applyEnv applies the environment mode and scrub list. With no mode, an
// explicit scrub list extends the profile's set: the standard profile starts
// with DefaultScrubEnv, custom starts empty.
func applyEnv(policy *sandbox.Policy, env *config.SandboxEnv) {
	if env == nil {
		return
	}

	switch env.Mode {
	case sandbox.EnvScrub:
		policy.ScrubEnv = append(sandbox.DefaultScrubEnv(), env.Scrub...)
	case sandbox.EnvInherit:
		policy.ScrubEnv = nil
	}

	if env.Mode == "" && len(env.Scrub) > 0 {
		policy.ScrubEnv = append(policy.ScrubEnv, env.Scrub...)
	}
}

// applyViolations gives the standard profile its default reporting and applies
// overrides. Custom starts with reporting off and enables only what it names.
func applyViolations(policy *sandbox.Policy, violations *config.SandboxViolations, standardDefaults bool) {
	if standardDefaults {
		policy.ViolationMode = sandbox.ViolationHeuristic
		policy.ReportToModel = true
	}

	if violations == nil {
		return
	}

	if violations.Mode != "" {
		policy.ViolationMode = violations.Mode
	}

	if violations.ReportToModel != nil {
		policy.ReportToModel = *violations.ReportToModel
	}
}
