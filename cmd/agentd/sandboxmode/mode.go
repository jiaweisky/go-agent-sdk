// Package sandboxmode defines the configuration values shared by the sandbox
// implementation and the config validator.
package sandboxmode

// Filesystem modes.
const (
	// FSModeAllow confines access to an allow-list.
	FSModeAllow = "allow"
	// FSModeOff disables confinement for that dimension.
	FSModeOff = "off"
)

// Network modes.
const (
	// NetworkOff disables network access for sandboxed commands.
	NetworkOff = "off"
	// NetworkNoEgress allows local traffic but blocks outbound connections.
	NetworkNoEgress = "no-egress"
)

// UNIX-socket modes. A socket is a filesystem object, so the sandbox enforces
// these through the file policy rather than a socket-specific filter.
const (
	// SocketInherit follows the filesystem policy.
	SocketInherit = "inherit"
	// SocketAllowAll disables the socket restriction.
	SocketAllowAll = "allowAll"
	// SocketAllowlist explicitly allows the listed socket paths.
	SocketAllowlist = "allowlist"
	// SocketDeny is the pre-rename name for SocketInherit.
	SocketDeny = "deny"
)

// Environment modes.
const (
	// EnvScrub removes the configured environment-name globs.
	EnvScrub = "scrub"
	// EnvInherit passes the environment through unchanged.
	EnvInherit = "inherit"
)

// Violation-reporting modes.
const (
	// ViolationHeuristic scans a command's stderr for OS denial messages.
	ViolationHeuristic = "heuristic"
	// ViolationPrecise parses syscall-level denials (Linux, via strace).
	ViolationPrecise = "precise"
	// ViolationOff disables command-side reporting.
	ViolationOff = "off"
)
