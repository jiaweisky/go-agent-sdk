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

	// Routing: optional; several servers may share one Listen when they differ
	// by Host or Mount. A server with neither serves the listener root.
	Host  string `yaml:"host"`
	Mount string `yaml:"mount"`
	// Default marks the A2A server that owns the origin-root agent card.
	Default bool `yaml:"default"`

	// Client-level options shared by both kinds.
	MediaUpload        *MediaUploadConfig `yaml:"mediaUpload"`
	MCP                []MCPServer        `yaml:"mcp"`
	References         []Reference        `yaml:"references"`
	InboundPermissions []PermissionRule   `yaml:"inboundPermissions"`
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

// SandboxConfig selects the command-execution seam.
type SandboxConfig struct {
	// Process names the execution seam: none (default) or a registered wrapper.
	Process string `yaml:"process" json:"process"`
	// FailurePolicy is what to do when the named sandbox cannot be built:
	// fail-closed (default) refuses to start; fail-open warns and runs without
	// the sandbox.
	FailurePolicy string `yaml:"failurePolicy" json:"failurePolicy"`
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
