package config

// Server is one served instance: one client. Servers with the same Listen share
// one listener and are routed by Host and PathPrefix.
type Server struct {
	Name string `yaml:"name"`
	Kind Kind   `yaml:"kind"` // KindHTTP | KindA2A
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
	Peers []A2APeer `yaml:"peers"`

	// Routing: optional; several servers may share one Listen when they differ
	// by Host or PathPrefix. A server with neither serves the listener root.
	Host string `yaml:"host"`
	// PathPrefix is a URL path prefix (not a filesystem mount); requests below
	// it are served by this server. It must be a clean, decoded path:
	// percent-encoded values and dot segments are rejected.
	PathPrefix string `yaml:"pathPrefix"`
	// Default marks the A2A server that owns the origin-root agent card.
	Default bool `yaml:"default"`

	// Client-level options shared by both kinds.
	MediaUpload        *MediaUploadConfig   `yaml:"mediaUpload"`
	Media              *MediaDeliveryConfig `yaml:"media"`
	Storage            *StorageConfig       `yaml:"storage"`
	MCP                []MCPServer          `yaml:"mcp"`
	References         []Reference          `yaml:"references"`
	InboundPermissions []PermissionRule     `yaml:"inboundPermissions"`
	// Questions selects the question tool's policy: QuestionsPark or
	// QuestionsDecline. The default depends on the kind: QuestionsDecline for
	// a2a (a remote A2A client only polls task state, so a parked question never
	// gets an answer), QuestionsPark for http.
	Questions     QuestionsPolicy `yaml:"questions"`
	SubagentDepth int             `yaml:"subagentDepth"`
	TitleAuto     bool            `yaml:"titleAuto"`
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
	InboundMedia InboundMediaPolicy    `yaml:"inboundMedia"`

	// KeepAlive is the SSE heartbeat; empty means the 15s default, "0s" disables.
	KeepAlive string `yaml:"keepAlive"`
	// MaxBodyBytes caps an HTTP request body (0 = a 128 MiB default; negative is
	// rejected). Guards against an oversized inline media payload.
	MaxBodyBytes int64 `yaml:"maxBodyBytes"`
}

// QuestionsPolicy selects how the question tool behaves.
type QuestionsPolicy string

const (
	// QuestionsPark holds a question at input-required until the client
	// answers.
	QuestionsPark QuestionsPolicy = "park"
	// QuestionsDecline rejects a question so the turn ends with the dismissal
	// instead of parking.
	QuestionsDecline QuestionsPolicy = "decline"
)

// Valid reports whether p is a supported question policy.
func (p QuestionsPolicy) Valid() bool {
	return p == "" || p == QuestionsPark || p == QuestionsDecline
}

// InboundMediaPolicy selects how inbound A2A media is delivered.
type InboundMediaPolicy string

const (
	// InboundMediaSpool writes inbound media files into the workspace.
	InboundMediaSpool InboundMediaPolicy = "spool"
	// InboundMediaInline delivers known media as native model parts when the
	// model accepts the modality.
	InboundMediaInline InboundMediaPolicy = "inline"
)

// Valid reports whether p is a supported inbound-media policy.
func (p InboundMediaPolicy) Valid() bool {
	return p == "" || p == InboundMediaSpool || p == InboundMediaInline
}

// IsolationMode selects how a per-server view of the working directory is made.
type IsolationMode string

const (
	// IsolationNone shares the working directory directly; no view is created.
	IsolationNone IsolationMode = "none"
	// IsolationCopy copies the source working directory into a per-instance
	// view.
	IsolationCopy IsolationMode = "copy"
	// IsolationWorktree creates a git worktree, starting from BaseRef, as the
	// instance view.
	IsolationWorktree IsolationMode = "worktree"
	// IsolationOverlay creates a copy-on-write overlay over the read-only
	// source.
	IsolationOverlay IsolationMode = "overlay"
	// IsolationContainer runs the instance in a container; the host provisions
	// it, not the local runner.
	IsolationContainer IsolationMode = "container"
	// IsolationRemote runs the instance on a remote host; the host provisions
	// it, not the local runner.
	IsolationRemote IsolationMode = "remote"
)

// Valid reports whether m is a supported isolation mode.
func (m IsolationMode) Valid() bool {
	switch m {
	case "", IsolationNone, IsolationCopy, IsolationWorktree, IsolationOverlay,
		IsolationContainer, IsolationRemote:
		return true
	}
	return false
}

// CowMode selects the overlay copy-on-write mechanism.
type CowMode string

const (
	// CowAuto lets the platform pick its available mechanism: reflink on Linux,
	// clonefile on macOS.
	CowAuto CowMode = "auto"
	// CowReflink uses a filesystem reflink copy (Linux).
	CowReflink CowMode = "reflink"
	// CowClonefile uses APFS clonefile (macOS).
	CowClonefile CowMode = "clonefile"
)

// Valid reports whether m is a supported copy-on-write mode.
func (m CowMode) Valid() bool {
	return m == "" || m == CowAuto || m == CowReflink || m == CowClonefile
}

// IsolationConfig derives a per-server view of the working directory.
type IsolationConfig struct {
	// Mode is IsolationNone, IsolationCopy, IsolationWorktree or IsolationOverlay.
	Mode IsolationMode `yaml:"mode" json:"mode"`
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
	// Mode is CowAuto, CowReflink or CowClonefile.
	Mode CowMode `yaml:"mode" json:"mode"`
}

// SandboxConfig selects the command-execution seam and its policy.
type SandboxConfig struct {
	// Wrapper selects the sandbox implementation. WrapperNone disables the
	// sandbox; omit the whole block to keep the SDK pass-through.
	Wrapper Wrapper `yaml:"wrapper" json:"wrapper"`
	// FailurePolicy is what to do when the named sandbox cannot be built:
	// FailurePolicyFailClosed (default) refuses to start; FailurePolicyFailOpen
	// warns and runs without the sandbox.
	FailurePolicy FailurePolicy `yaml:"failurePolicy" json:"failurePolicy"`
	// The policy block (profile, filesystem, network, …) is inlined.
	SandboxPolicy `yaml:",inline"`
}

// SandboxPolicy is the shared sandbox policy block. A "standard" profile applies
// secure defaults; "custom" applies only the explicit fields; "off" disables the
// policy (and the sandbox itself).
type SandboxPolicy struct {
	Profile           Profile             `yaml:"profile" json:"profile,omitempty"`
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
