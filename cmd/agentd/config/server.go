package config

import (
	"errors"
	"fmt"
	"time"
)

// Server is one served instance: one client. Servers with the same Listen share
// one listener and are routed by Host and Mount.
type Server struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"` // http | a2a
	// Listen is the bind address; Root is the SDK state directory.
	Listen string `yaml:"listen"`
	Root   string `yaml:"root"`
	// WorkingDirectory is the project/worktree root. Empty means the process cwd.
	WorkingDirectory string   `yaml:"workingDirectory"`
	ConfigDirs       []string `yaml:"configDirs"`
	// Isolation, when set, derives a per-server view of the working directory so
	// several servers can share one source project safely.
	Isolation *IsolationConfig `yaml:"isolation"`
	// Sandbox selects the command-execution seam for this server's tools.
	Sandbox *SandboxConfig `yaml:"sandbox"`
	// Models is the subset of the global catalog this server exposes; empty
	// means the whole catalog.
	Models       []string `yaml:"models"`
	DefaultModel string   `yaml:"defaultModel"`
	Agents       []string `yaml:"agents"`
	// Peers are remote A2A agents this server reaches as a client; each is
	// registered as an agent the local model can delegate to.
	Peers []Peer `yaml:"peers"`

	// Routing: optional; several servers may share one Listen when they differ
	// by Host or Mount. A server with neither serves the listener root.
	Host  string `yaml:"host"`
	Mount string `yaml:"mount"`
	// Default marks the A2A server that owns the origin-root agent card.
	Default bool `yaml:"default"`

	// Client-level options shared by both kinds.
	MediaUpload        *MediaUploadConfig  `yaml:"mediaUpload"`
	Media              *InboundMediaConfig `yaml:"media"`
	Storage            *StorageConfig      `yaml:"storage"`
	MCP                []MCPServer         `yaml:"mcp"`
	References         []Reference         `yaml:"references"`
	InboundPermissions []PermissionRule    `yaml:"inboundPermissions"`
	// Questions selects the question tool's policy: "park" or "decline". The
	// default depends on the kind: "decline" for a2a (a remote A2A client only
	// polls task state, so a parked question never gets an answer), "park" for
	// http.
	Questions     string `yaml:"questions"`
	SubagentDepth int    `yaml:"subagentDepth"`
	TitleAuto     bool   `yaml:"titleAuto"`
	// DropErroredTurns defaults to true when unset.
	DropErroredTurns *bool `yaml:"dropErroredTurns"`

	Auth          *AuthConfig `yaml:"auth"`
	TLS           *TLS        `yaml:"tls"`
	AllowInsecure bool        `yaml:"allowInsecure"`

	// HTTP-only.
	InteractionTimeout  string   `yaml:"interactionTimeout"`
	InteractionFallback string   `yaml:"interactionFallback"`
	SubscriberCapacity  int      `yaml:"subscriberCapacity"`
	PublicRoutes        []string `yaml:"publicRoutes"`
	CORS                []string `yaml:"cors"`

	// A2A-only.
	BaseURL      string                `yaml:"baseURL"`
	Cards        map[string]CardConfig `yaml:"cards"`
	InboundMedia string                `yaml:"inboundMedia"`

	// KeepAlive is the SSE heartbeat; empty means the 15s default, "0s" disables.
	KeepAlive string `yaml:"keepAlive"`
	// MaxBodyBytes caps an HTTP request body (0 = a 128 MiB default; negative is
	// rejected). Guards against an oversized inline media payload.
	MaxBodyBytes int64 `yaml:"maxBodyBytes"`
}

// AuthConfig enables authentication on the served endpoints.
type AuthConfig struct {
	Bearer   string `yaml:"bearer"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// IsolationConfig derives a per-server view of the working directory.
type IsolationConfig struct {
	// Mode is none | copy | worktree | overlay.
	Mode string `yaml:"mode" json:"mode"`
	// BaseDir is where views are created (required by every mode but none).
	BaseDir string `yaml:"baseDir" json:"baseDir"`
	// BaseReadOnly treats the source as read-only (required by overlay).
	BaseReadOnly bool `yaml:"baseReadOnly" json:"baseReadOnly"`
	// BaseRef is the commit-ish a worktree starts from; empty means HEAD.
	BaseRef string `yaml:"baseRef" json:"baseRef"`
	// Cow selects the overlay copy-on-write mechanism.
	Cow *CowConfig `yaml:"cow" json:"cow"`
}

// CowConfig selects the overlay copy-on-write mechanism.
type CowConfig struct {
	// Mode is auto | reflink | clonefile.
	Mode string `yaml:"mode" json:"mode"`
}

// SandboxConfig selects the command-execution seam and its policy.
type SandboxConfig struct {
	// Process names the execution seam: none (default) or a registered wrapper.
	Process string `yaml:"process" json:"process"`
	// FailurePolicy is what to do when the named sandbox cannot be built:
	// fail-closed (default) refuses to start; fail-open warns and runs without
	// the sandbox.
	FailurePolicy string `yaml:"failurePolicy" json:"failurePolicy"`
	// The policy block (profile, filesystem, network, …) is inlined.
	SandboxPolicy `yaml:",inline"`
}

// SandboxPolicy is the shared sandbox policy block. A "standard" profile applies
// secure defaults; "custom" applies only the explicit fields; "off" disables the
// policy (and the sandbox itself).
type SandboxPolicy struct {
	Profile           string              `yaml:"profile" json:"profile,omitempty"`
	Filesystem        *SandboxFilesystem  `yaml:"filesystem" json:"filesystem,omitempty"`
	Network           *SandboxNetwork     `yaml:"network" json:"network,omitempty"`
	UnixSockets       *SandboxUnixSockets `yaml:"unixSockets" json:"unixSockets,omitempty"`
	ProcessInspection string              `yaml:"processInspection" json:"processInspection,omitempty"`
	Env               *SandboxEnv         `yaml:"env" json:"env,omitempty"`
	Violations        *SandboxViolations  `yaml:"violations" json:"violations,omitempty"`
}

// SandboxViolations controls violation reporting. Mode is "heuristic" (the
// default: best-effort command-side detection), "precise" (Linux syscall
// tracing) or "off". ReportToModel appends a denial note to a command's output.
type SandboxViolations struct {
	Mode          string `yaml:"mode" json:"mode,omitempty"`
	ReportToModel *bool  `yaml:"reportToModel" json:"reportToModel,omitempty"`
}

// SandboxFilesystem declares the file policy. Read/Write are "allow" (an
// allow-list) or "off" (no confinement); empty inherits the profile. Windows
// cannot express "off" and validation rejects it.
type SandboxFilesystem struct {
	Read       string   `yaml:"read" json:"read,omitempty"`
	Write      string   `yaml:"write" json:"write,omitempty"`
	DenyRead   []string `yaml:"denyRead" json:"denyRead,omitempty"`
	AllowRead  []string `yaml:"allowRead" json:"allowRead,omitempty"`
	DenyWrite  []string `yaml:"denyWrite" json:"denyWrite,omitempty"`
	AllowWrite []string `yaml:"allowWrite" json:"allowWrite,omitempty"`
}

// SandboxNetwork declares the network policy. Only off and no-egress are
// implemented; a domain allow-list needs a proxy.
type SandboxNetwork struct {
	Mode           string   `yaml:"mode" json:"mode,omitempty"`
	AllowedDomains []string `yaml:"allowedDomains" json:"allowedDomains,omitempty"`
}

// SandboxUnixSockets declares the UNIX-socket policy. Mode is "inherit" (the
// default: follow the filesystem policy), "allowAll" (no socket-specific policy;
// the filesystem policy still applies) or "allowlist"; "deny" is accepted as a
// legacy alias for inherit.
type SandboxUnixSockets struct {
	Mode  string   `yaml:"mode" json:"mode,omitempty"`
	Allow []string `yaml:"allow" json:"allow,omitempty"`
}

// SandboxEnv declares the child-environment policy.
type SandboxEnv struct {
	Mode  string   `yaml:"mode" json:"mode,omitempty"`
	Scrub []string `yaml:"scrub" json:"scrub,omitempty"`
}

// KeepAliveDuration parses a keepAlive value; empty means the 15s default
// (0 disables).
func KeepAliveDuration(keepAlive string) (time.Duration, error) {
	if keepAlive == "" {
		return 15 * time.Second, nil
	}
	duration, err := time.ParseDuration(keepAlive)
	if err != nil {
		return 0, fmt.Errorf("keepAlive: %w", err)
	}
	if duration < 0 {
		return 0, errors.New("keepAlive must not be negative")
	}
	return duration, nil
}

// InteractionTimeout parses an interactionTimeout value; empty means no timeout.
func InteractionTimeout(interactionTimeout string) (time.Duration, error) {
	if interactionTimeout == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(interactionTimeout)
	if err != nil {
		return 0, fmt.Errorf("interactionTimeout: %w", err)
	}
	if duration < 0 {
		return 0, errors.New("interactionTimeout must not be negative")
	}
	return duration, nil
}
