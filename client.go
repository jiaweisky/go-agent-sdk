package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

// ClientOptions configures a Client.
type ClientOptions struct {
	// Name is the application name reported to MCP servers as the client
	// identity (MCP clientInfo). Empty uses DefaultName.
	Name string
	// WorkingDirectory is the project/worktree root. Defaults to the process cwd.
	WorkingDirectory string
	// Root is the base directory for every piece of SDK-managed state. It is
	// required: the SDK never picks a location (and never reads the environment
	// to find one), so the host always knows what it is reading and writing.
	// Pointing each instance at its own Root isolates it (tests, sandboxes,
	// multiple tenants).
	//
	//	{Root}/data/   session database, snapshots, references, tool output, logs
	//	{Root}/cache/  downloaded binaries and skill content
	//	{Root}/state/  runtime state (reserved)
	//	{Root}/tmp/    scratch space
	Root string
	// DatabaseMaxConns overrides the SQLite connection-pool size. Zero uses the
	// default (database.DefaultMaxConns). WAL keeps reads concurrent; writes
	// always serialize.
	DatabaseMaxConns int
	// ConfigDirs are explicit directories to load agents and skills from. Each
	// is scanned for {agent,agents}/**/*.md, {mode,modes}/*.md and
	// {skill,skills}/**/SKILL.md. No environment, global or project discovery is
	// performed.
	ConfigDirs []string
	// Home is the user's home directory, used to expand ~ in agent permission
	// rules. Empty uses the OS home directory.
	Home string

	// Model is the default model id used when a session does not override it.
	Model string
	// Protocol selects the wire protocol when ModelInfo is not set, for
	// example "openai-responses", "anthropic-messages" or "gemini".
	Protocol schema.ProtocolID
	// ModelInfo resolves a model id to catalog-style metadata (protocol, limits,
	// endpoint, headers, variants), so callers can feed models.dev (or their
	// own) metadata. When set it takes precedence over Protocol. Build the value
	// with NewModelInfo for the SDK's supported protocols.
	ModelInfo ModelInfoProvider
	// ContextLimit overrides the model context window (in tokens) the SDK uses
	// to decide when automatic compaction runs. Zero keeps the ModelInfo limit;
	// when neither is set, automatic compaction is disabled.
	ContextLimit int
	// OutputLimit overrides the model output token budget, which sizes the
	// compaction summary. Zero keeps the ModelInfo limit.
	OutputLimit int
	// Credentials supplies the provider secret used to authenticate requests.
	// The protocol decides how it is applied, so callers supply only the secret.
	Credentials CredentialProvider
	// LLM overrides the default LLM client (route.LLMClient). When nil, a client
	// is built from ModelInfo/Protocol over the default HTTP transport.
	LLM route.LLMClient

	// Tools are application-level tools exposed to every session.
	Tools []Tool
	// MCP configures MCP servers whose tools are registered as application tools.
	MCP []MCPConfig
	// Plugins are loaded at startup and can transform agents and skills.
	Plugins []PluginConfig
	// References are named reference sources injected into the system context.
	References map[string]core.ReferenceSource
	// ToolOutput bounds oversized tool results. Nil uses the defaults.
	ToolOutput *ToolOutputConfig

	// Permission is the default permission handler. Defaults to ApproveAll.
	Permission PermissionHandlerFunc
	// Question is the default question handler. Defaults to RejectQuestions.
	Question QuestionHandlerFunc
	// OnPanic is called when a user event handler panics; the panic is
	// recovered either way. Nil ignores it.
	OnPanic func(PanicInfo)
	// SubagentDepth limits nested subagent (task) recursion. Defaults to 1.
	SubagentDepth int
	// Shell overrides the shell used by the bash tool. Empty uses the platform
	// default.
	Shell string
	// Locale orders model-visible lists (read-tool directory listings, skill and
	// reference guidance, advertised subagent types) with locale-aware
	// collation. Empty uses the environment locale (LC_ALL, LC_COLLATE, LANG).
	Locale string
	// Compaction controls automatic history compaction. Nil uses the defaults.
	Compaction *CompactionConfig
	// Title controls automatic session title generation. Nil keeps it disabled.
	Title *TitleConfig
}

// TitleConfig controls automatic session title generation. The SDK runs the
// built-in "title" agent over the first user message once per session and
// stores the result as the session title.
type TitleConfig struct {
	// Auto enables title generation. Nil keeps the default (disabled).
	Auto *bool
	// Model overrides the model used to generate titles, taking priority over
	// the built-in "title" agent's own model. Empty falls back to the title
	// agent's model and then to the session's model.
	Model string
}

// DefaultName is the application name reported to MCP servers when
// ClientOptions.Name is empty.
const DefaultName = "go-agent-sdk"

// ToolOutputConfig bounds oversized tool results. Zero values use the defaults.
type ToolOutputConfig struct {
	MaxLines int
	MaxBytes int
}

// CompactionConfig controls automatic history compaction.
type CompactionConfig struct {
	// Auto enables automatic compaction. Nil keeps the default (enabled).
	Auto *bool
	// PreserveRecentTokens is the token budget of history kept verbatim after
	// compaction. Zero keeps the default.
	PreserveRecentTokens int
	// Reserved is the headroom subtracted from the model context limit. Zero
	// keeps the default.
	Reserved int
}

// ModelInfoProvider resolves a model id to catalog-style metadata. It is the
// SDK's narrow replacement for the models.dev catalog: the caller supplies the
// data (limits, endpoint, headers, variants) it already has.
type ModelInfoProvider func(modelID string) (schema.ModelInfo, error)

// CredentialProvider returns the credential for a provider id.
type CredentialProvider func(providerID schema.ProviderID) (schema.Credential, error)

// APIKey returns a CredentialProvider that supplies the same API key for every
// provider.
func APIKey(key string) CredentialProvider {
	return func(schema.ProviderID) (schema.Credential, error) {
		return schema.Credential{APIKey: key}, nil
	}
}

// MCPConfig names an MCP server and its connection config.
type MCPConfig struct {
	Name   string
	Server mcp.ServerConfig
}

// PluginConfig registers a plugin loaded at client startup.
type PluginConfig struct {
	ID string
	// Options is exposed to the plugin as PluginContext.Options.
	Options map[string]any
	// Tools are application tools contributed by this plugin. They are
	// registered for every session created after startup, like
	// ClientOptions.Tools.
	Tools  []Tool
	Effect PluginEffect
}

// AgentConfig defines a named agent.
type AgentConfig struct {
	System      string
	Description string
	// Mode is "primary", "subagent", or "all". When nil the agent keeps its
	// current mode; a newly created agent defaults to "all".
	Mode *string
	// Hidden hides the agent from default selection. When nil the agent keeps
	// its current value.
	Hidden      *bool
	Steps       int
	Model       string
	Permissions schema.PermissionRuleset
}

// Client owns the process-wide services shared by sessions.
type Client struct {
	opts ClientOptions
	rt   *runtime

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewClient builds a client and its backing runtime.
func NewClient(opts *ClientOptions) (*Client, error) {
	return NewClientContext(context.Background(), opts)
}

// NewClientContext is NewClient with a context used for startup work such as
// connecting MCP servers.
func NewClientContext(ctx context.Context, opts *ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &ClientOptions{}
	}
	rt, err := newRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Client{opts: *opts, rt: rt, sessions: map[string]*Session{}}, nil
}

// RegisterAgent registers or updates a named agent.
func (c *Client) RegisterAgent(name string, cfg AgentConfig) error {
	_, err := c.rt.agents.Transform(func(draft core.AgentDraft) error {
		draft.Update(schema.AgentID(name), func(info *schema.AgentInfo) {
			applyAgentConfig(info, cfg)
		})
		return nil
	})
	return err
}

// applyAgentConfig merges the non-zero fields of cfg into info.
func applyAgentConfig(info *schema.AgentInfo, cfg AgentConfig) {
	if cfg.System != "" {
		system := cfg.System
		info.System = &system
	}
	if cfg.Description != "" {
		description := cfg.Description
		info.Description = &description
	}
	if cfg.Mode != nil {
		info.Mode = *cfg.Mode
	}
	if cfg.Hidden != nil {
		info.Hidden = *cfg.Hidden
	}
	if cfg.Steps > 0 {
		steps := schema.PositiveInt(cfg.Steps)
		info.Steps = &steps
	}
	if cfg.Model != "" {
		info.Model = &schema.ModelRef{ID: schema.ModelID(cfg.Model)}
	}
	if cfg.Permissions != nil {
		info.Permissions = core.PermissionMerge(info.Permissions, cfg.Permissions)
	}
}

// CreateSession creates a new session bound to an agent. The context is
// accepted for API symmetry; session creation is synchronous and does not
// currently observe cancellation.
func (c *Client) CreateSession(ctx context.Context, cfg *SessionConfig) (*Session, error) {
	if cfg == nil {
		cfg = &SessionConfig{}
	}
	sessionID := schema.NewSessionID()
	agentID, modelID := c.sessionDefaults(cfg)
	effectiveAgent, derivedAgent, err := c.deriveAgent(cfg, sessionID, agentID, modelID)
	if err != nil {
		return nil, err
	}
	sessionRegistry, err := c.rt.newSessionRegistry(effectiveAgent, cfg.Permissions, cfg.Tools)
	if err != nil {
		return nil, err
	}
	// The row keeps the durable base agent; the derived copy lives in the
	// runtime selector for as long as the session does.
	if err := c.insertSession(cfg, sessionID, agentID, modelID); err != nil {
		return nil, err
	}
	return c.newSession(sessionID, effectiveAgent, derivedAgent, sessionRegistry, cfg)
}

// newSession registers a session's derived agent (when it has one), builds its
// handle and attaches it to the runtime.
func (c *Client) newSession(
	id schema.SessionID,
	effectiveAgent *schema.AgentInfo,
	derivedAgent bool,
	registry *tool.Registry,
	cfg *SessionConfig,
) (*Session, error) {
	session := &Session{
		client: c, id: id, agentID: string(effectiveAgent.ID),
		hooks: cfg.Hooks, listeners: map[int]func(Event){},
	}
	if err := c.attachSession(session, registry, cfg); err != nil {
		return nil, err
	}
	if derivedAgent {
		c.rt.agentSelector.set(id, *effectiveAgent)
	}
	return session, nil
}

// attachSession wires a session into the runtime: its tool registry, hooks,
// per-session handlers and event subscription. It reserves the session id
// first, so a second attach for the same id fails instead of silently
// overwriting the first handle's state.
func (c *Client) attachSession(session *Session, registry *tool.Registry, cfg *SessionConfig) error {
	if err := c.trackSession(session); err != nil {
		return err
	}
	if err := c.rt.setSessionRegistry(session.ID(), registry); err != nil {
		c.untrackSession(session)
		return err
	}
	c.rt.setHooks(session.ID(), cfg.Hooks)
	c.rt.permission.setHandler(session.ID(), cfg.OnPermissionRequest)
	c.rt.question.setHandler(session.ID(), cfg.OnQuestion)
	unsubscribe, err := c.rt.events.Listen(func(payload schema.EventPayload) error {
		session.dispatch(payload)
		return nil
	})
	if err != nil {
		c.rt.detach(session.ID())
		c.untrackSession(session)
		return err
	}
	session.unsub = unsubscribe
	return nil
}

// sessionDefaults is the session's agent and model, with the client's defaults
// filled in.
func (c *Client) sessionDefaults(cfg *SessionConfig) (agentID, modelID string) {
	agentID = cfg.Agent
	if agentID == "" {
		agentID = string(core.AgentDefaultID)
	}
	modelID = cfg.Model
	if modelID == "" {
		modelID = c.opts.Model
	}
	return agentID, modelID
}

// deriveAgent validates the named agent and returns the agent the session runs
// with. When the session customizes the system prompt, it returns an in-memory
// copy with a session-scoped id so the shared agent is not mutated.
func (c *Client) deriveAgent(
	cfg *SessionConfig,
	sessionID schema.SessionID,
	agentID, modelID string,
) (*schema.AgentInfo, bool, error) {
	base := c.rt.agents.Get(schema.AgentID(agentID))
	if base == nil {
		return nil, false, fmt.Errorf("unknown agent: %s", agentID)
	}
	startContext, err := runSessionStartHook(cfg, sessionID, agentID, modelID)
	if err != nil {
		return nil, false, err
	}
	if cfg.SystemPrompt == nil && startContext == "" {
		return base, false, nil
	}
	copied := *base
	copied.ID = schema.AgentID(agentID + "#" + string(sessionID))
	applySystemPrompt(&copied, cfg.SystemPrompt)
	if startContext != "" {
		system := ""
		if copied.System != nil {
			system = *copied.System
		}
		combined := joinSections(system, startContext)
		copied.System = &combined
	}
	return &copied, true, nil
}

// runSessionStartHook runs OnSessionStart and returns its additional context.
func runSessionStartHook(cfg *SessionConfig, sessionID schema.SessionID, agentID, modelID string) (string, error) {
	if cfg.Hooks == nil || cfg.Hooks.OnSessionStart == nil {
		return "", nil
	}
	out, err := cfg.Hooks.OnSessionStart(SessionStartInput{
		SessionID: string(sessionID), Agent: agentID, Model: modelID,
	})
	if err != nil {
		return "", err
	}
	if out == nil {
		return "", nil
	}
	return out.AdditionalContext, nil
}

// MCP returns the MCP client, or nil when no MCP servers are configured.
func (c *Client) MCP() *mcp.Client {
	return c.rt.mcp
}

// Plugin returns the plugin manager.
func (c *Client) Plugin() *core.Plugin {
	return c.rt.plugin
}

// RegisterTools adds application tools after startup. They are registered for
// sessions created afterwards and for live sessions. The returned function
// unregisters them.
func (c *Client) RegisterTools(tools ...Tool) (func(), error) {
	return c.rt.toolRegistrar().Register(tools...)
}

// Agents returns the agent registry (built-ins plus config-registered agents).
func (c *Client) Agents() *core.Agent {
	return c.rt.agents
}

// Skills returns the skill service.
func (c *Client) Skills() *core.Skill {
	return c.rt.skills
}

// On subscribes to every session event, including subagent child sessions.
// Use it to observe task/subagent activity that Session.On (which filters by
// session) does not surface. The returned function unsubscribes.
func (c *Client) On(handler func(Event)) func() {
	return listenEvents(c.rt.events, handler, c.rt.onPanic)
}

// Reference returns the reference service.
func (c *Client) Reference() *core.Reference {
	return c.rt.reference
}

// CleanupToolOutput removes tool-output files older than the retention window.
func (c *Client) CleanupToolOutput() error {
	if c.rt == nil || c.rt.toolOutput == nil {
		return nil
	}
	return c.rt.toolOutput.Cleanup()
}

// Capture returns the current snapshot id for the project, or false when the
// project has no snapshot support or the best-effort capture failed. Pair two
// captures around a turn to diff what it changed, independent of step events.
func (c *Client) Capture() (string, bool) {
	if c.rt == nil || c.rt.snapshot == nil {
		return "", false
	}
	id, err := c.rt.snapshot.Capture()
	if err != nil || id == nil {
		return "", false
	}
	return string(*id), true
}

// Diff returns the per-file diffs between two snapshot ids. It returns nil when
// the project has no snapshot support or either id is empty.
func (c *Client) Diff(from, to string) ([]core.FileDiff, error) {
	if c.rt == nil || c.rt.snapshot == nil || from == "" || to == "" {
		return nil, nil
	}
	return c.rt.snapshot.Diff(core.SnapshotDiffInput{
		SnapshotCompareInput: core.SnapshotCompareInput{From: core.SnapshotID(from), To: core.SnapshotID(to)},
	})
}

// Close closes active sessions and releases the runtime and its database.
func (c *Client) Close() error {
	c.mu.Lock()
	sessions := make([]*Session, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = map[string]*Session{}
	c.mu.Unlock()
	// Stop in-flight turns before tearing down sessions and the database, so a
	// running turn never touches a closed database.
	c.rt.stopTurns()
	for _, session := range sessions {
		_ = session.Close()
	}
	return c.rt.close()
}
