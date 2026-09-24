package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

// ClientOptions configures a Client.
type ClientOptions struct {
	// Model is the default model id used when a session does not override it.
	Model string
	// Protocol selects the wire protocol when ModelInfo is not set, for
	// example "openai-responses", "anthropic-messages" or "gemini".
	Protocol schema.ProtocolID
	// WorkingDirectory is the project/worktree root. Defaults to the process cwd.
	WorkingDirectory string
	// DataDir stores the session database. Empty uses the default opencode path.
	DataDir string
	// DataRoot overrides the data directory holding snapshots, references and
	// managed tool output. Empty uses the default opencode path
	// ($XDG_DATA_HOME/opencode or ~/.local/share/opencode). It is independent of
	// DataDir, which only stores the session database.
	DataRoot string
	// Tools are application-level tools exposed to every session.
	Tools []Tool
	// MCP configures MCP servers whose tools are registered as application tools.
	MCP []MCPConfig
	// Plugins are loaded at startup and can transform agents and skills.
	Plugins []PluginConfig
	// References are named reference sources injected into the system context.
	References map[string]core.ReferenceSource
	// Permission is the default permission handler. Defaults to ApproveAll.
	Permission PermissionHandlerFunc
	// Question is the default question handler. Defaults to RejectQuestions.
	Question QuestionHandlerFunc

	// LLM overrides the default LLM client (route.LLMClient). When nil, a client
	// is built from Provider/Model over the default HTTP transport.
	LLM route.LLMClient

	// ModelInfo resolves a model id to catalog-style metadata (protocol, limits,
	// endpoint, headers, variants), so callers can feed models.dev (or their
	// own) metadata. When set it takes precedence over Protocol. Build the value
	// with NewModelInfo for the SDK's supported protocols.
	ModelInfo ModelInfoProvider

	// Credentials supplies the provider secret used to authenticate requests.
	// The protocol decides how it is applied, so callers supply only the secret.
	Credentials CredentialProvider

	// ConfigDirs are explicit directories to load agents and skills from. Each
	// is scanned for {agent,agents}/**/*.md, {mode,modes}/*.md and
	// {skill,skills}/**/SKILL.md. No environment, global or project discovery is
	// performed.
	ConfigDirs []string
	// SubagentDepth limits nested subagent (task) recursion. Defaults to 1.
	SubagentDepth int
	// Shell overrides the shell used by the bash tool. Empty uses the platform
	// default.
	Shell string
	// ToolOutput bounds oversized tool results. Nil uses the defaults.
	ToolOutput *ToolOutputConfig
	// Compaction controls automatic history compaction. Nil uses the defaults.
	Compaction *CompactionConfig
	// Policies are policy statements evaluated for tool access.
	Policies []schema.PolicyInfo
}

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
	ID     string
	Effect func(*core.PluginContext) (func(), error)
}

// AgentConfig defines a named agent.
type AgentConfig struct {
	System      string
	Description string
	// Mode is "primary", "subagent", or "all". When nil the agent keeps its
	// current mode; a newly created agent defaults to "all" (matching
	// opencode's Agent.Info.empty).
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
	sessions map[*Session]struct{}
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
	return &Client{opts: *opts, rt: rt, sessions: map[*Session]struct{}{}}, nil
}

// RegisterAgent registers or updates a named agent.
func (c *Client) RegisterAgent(name string, cfg AgentConfig) error {
	_, err := c.rt.agents.Transform(func(draft core.AgentDraft) error {
		draft.Update(schema.AgentID(name), func(info *schema.AgentInfo) {
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
		})
		return nil
	})
	return err
}

// CreateSession creates a new session bound to an agent.
func (c *Client) CreateSession(ctx context.Context, cfg *SessionConfig) (*Session, error) {
	if cfg == nil {
		cfg = &SessionConfig{}
	}
	agentID := cfg.Agent
	if agentID == "" {
		agentID = string(core.AgentDefaultID)
	}
	modelID := cfg.Model
	if modelID == "" {
		modelID = c.opts.Model
	}
	sessionID := schema.NewSessionID()

	// Per-session system prompt customization is applied to a derived agent so
	// it does not mutate the shared agent used by other sessions.
	effectiveAgent := agentID
	derivedAgent := false
	var startContext string
	if cfg.Hooks != nil && cfg.Hooks.OnSessionStart != nil {
		if out, _ := cfg.Hooks.OnSessionStart(SessionStartInput{SessionID: string(sessionID), Agent: agentID, Model: modelID}); out != nil {
			startContext = out.AdditionalContext
		}
	}
	if cfg.SystemPrompt != nil || startContext != "" {
		base := c.rt.agents.Get(schema.AgentID(agentID))
		if base == nil {
			return nil, fmt.Errorf("unknown agent: %s", agentID)
		}
		effectiveAgent = agentID + "#" + string(sessionID)
		copied := *base
		applySystemPrompt(&copied, cfg.SystemPrompt)
		if startContext != "" {
			system := ""
			if copied.System != nil {
				system = *copied.System
			}
			combined := joinSections(system, startContext)
			copied.System = &combined
		}
		if _, err := c.rt.agents.Transform(func(draft core.AgentDraft) error {
			draft.Update(schema.AgentID(effectiveAgent), func(target *schema.AgentInfo) {
				*target = copied
			})
			return nil
		}); err != nil {
			return nil, err
		}
		derivedAgent = true
	}

	sessionRegistry, err := c.rt.newSessionRegistry(effectiveAgent, cfg.Permissions, cfg.Tools)
	if err != nil {
		c.removeDerivedAgent(effectiveAgent, derivedAgent)
		return nil, err
	}

	// The stored provider id is informational; resolution uses the resolved
	// ModelInfo. It is the configured protocol (empty when ModelInfo is set).
	providerID := string(c.opts.Protocol)
	now := time.Now().UnixMilli()
	title := cfg.Title
	if title == "" {
		title = "Session"
	}
	permissionJSON := "null"
	if len(cfg.Permissions) > 0 {
		if encoded, err := json.Marshal(cfg.Permissions); err == nil {
			permissionJSON = string(encoded)
		}
	}
	if _, err := c.rt.db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`permission`,`revert`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		string(sessionID), string(c.rt.projectID), "session", string(c.rt.location.Directory),
		title, "1", effectiveAgent, fmt.Sprintf(`{"id":%q,"providerID":%q}`, modelID, providerID), permissionJSON, `null`, now, now,
	); err != nil {
		c.removeDerivedAgent(effectiveAgent, derivedAgent)
		return nil, err
	}
	c.rt.dispatcher.set(string(sessionID), sessionRegistry)
	c.rt.setHooks(string(sessionID), cfg.Hooks)
	c.rt.permission.setHandler(string(sessionID), cfg.OnPermissionRequest)
	c.rt.question.setHandler(string(sessionID), cfg.OnQuestion)

	session := &Session{
		client: c, id: sessionID, agentID: effectiveAgent, derivedAgent: derivedAgent,
		hooks: cfg.Hooks, listeners: map[int]func(Event){},
	}
	unsub, err := c.rt.events.Listen(func(payload schema.EventPayload) error {
		session.dispatch(payload)
		return nil
	})
	if err != nil {
		c.removeDerivedAgent(effectiveAgent, derivedAgent)
		return nil, err
	}
	session.unsub = unsub
	c.trackSession(session)
	return session, nil
}

func (c *Client) removeDerivedAgent(agentID string, derived bool) {
	if !derived {
		return
	}
	_, _ = c.rt.agents.Transform(func(draft core.AgentDraft) error {
		draft.Remove(schema.AgentID(agentID))
		return nil
	})
}

// MCP returns the MCP client, or nil when no MCP servers are configured.
func (c *Client) MCP() *mcp.Client {
	return c.rt.mcp
}

// Plugin returns the plugin manager.
func (c *Client) Plugin() *core.Plugin {
	return c.rt.plugin
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
	unsubscribe, err := c.rt.events.Listen(func(payload schema.EventPayload) error {
		for _, event := range mapEvents(payload) {
			handler(event)
		}
		return nil
	})
	if err != nil {
		return func() {}
	}
	return unsubscribe
}

// Reference returns the reference service.
func (c *Client) Reference() *core.Reference {
	return c.rt.reference
}

// Close closes active sessions and releases the runtime and its database.
// CleanupToolOutput removes tool-output files older than the retention window.
func (c *Client) CleanupToolOutput() error {
	if c.rt == nil || c.rt.toolOutput == nil {
		return nil
	}
	return c.rt.toolOutput.Cleanup()
}

func (c *Client) Close() error {
	c.mu.Lock()
	sessions := make([]*Session, 0, len(c.sessions))
	for session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = map[*Session]struct{}{}
	c.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	return c.rt.close()
}

// SessionInfo summarizes a stored session.
type SessionInfo struct {
	ID          string
	Title       string
	Agent       string
	Directory   string
	TimeCreated int64
	TimeUpdated int64
}

// ResumeSession reattaches to an existing session.
func (c *Client) ResumeSession(ctx context.Context, sessionID string, cfg *SessionConfig) (*Session, error) {
	if cfg == nil {
		cfg = &SessionConfig{}
	}
	stored, err := c.rt.store.Get(schema.SessionID(sessionID))
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	// A per-session ruleset passed on resume replaces the stored one.
	sessionRules := schema.PermissionRuleset{}
	if stored.Permission != nil {
		sessionRules = *stored.Permission
	}
	if len(cfg.Permissions) > 0 {
		encoded, err := json.Marshal(cfg.Permissions)
		if err != nil {
			return nil, err
		}
		if _, err := c.rt.db.DB.Exec("UPDATE `session` SET `permission` = ? WHERE `id` = ?", string(encoded), sessionID); err != nil {
			return nil, err
		}
		sessionRules = cfg.Permissions
	}
	registry, err := c.rt.newSessionRegistry(agentIDOf(stored), sessionRules, cfg.Tools)
	if err != nil {
		return nil, err
	}
	c.rt.dispatcher.set(sessionID, registry)
	c.rt.setHooks(sessionID, cfg.Hooks)
	c.rt.permission.setHandler(sessionID, cfg.OnPermissionRequest)
	c.rt.question.setHandler(sessionID, cfg.OnQuestion)

	session := &Session{client: c, id: schema.SessionID(sessionID), hooks: cfg.Hooks, listeners: map[int]func(Event){}}
	unsubscribe, err := c.rt.events.Listen(func(payload schema.EventPayload) error {
		session.dispatch(payload)
		return nil
	})
	if err != nil {
		return nil, err
	}
	session.unsub = unsubscribe
	c.trackSession(session)
	if cfg.Hooks != nil && cfg.Hooks.OnSessionStart != nil {
		_, _ = cfg.Hooks.OnSessionStart(SessionStartInput{SessionID: sessionID, Agent: string(agentIDOf(stored)), Model: modelIDOf(stored)})
	}
	return session, nil
}

func (c *Client) trackSession(session *Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions == nil {
		c.sessions = map[*Session]struct{}{}
	}
	c.sessions[session] = struct{}{}
}

func (c *Client) untrackSession(session *Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, session)
}

// SessionMessages returns the stored messages of any session, including
// subagent child sessions.
func (c *Client) SessionMessages(sessionID string) ([]schema.SessionMessage, error) {
	return c.rt.store.Context(schema.SessionID(sessionID))
}

// ListSessions returns the sessions in the client's project, newest first.
func (c *Client) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	rows, err := c.rt.db.DB.Query(
		"SELECT `id`,`title`,`agent`,`directory`,`time_created`,`time_updated` FROM `session` WHERE `project_id` = ? ORDER BY `time_updated` DESC",
		string(c.rt.projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SessionInfo
	for rows.Next() {
		var info SessionInfo
		var agent *string
		if err := rows.Scan(&info.ID, &info.Title, &agent, &info.Directory, &info.TimeCreated, &info.TimeUpdated); err != nil {
			return nil, err
		}
		if agent != nil {
			info.Agent = *agent
		}
		result = append(result, info)
	}
	return result, rows.Err()
}

// DeleteSession permanently removes a session.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	if _, err := c.rt.db.DB.Exec("DELETE FROM `session` WHERE `id` = ?", sessionID); err != nil {
		return err
	}
	c.rt.dispatcher.remove(sessionID)
	return nil
}

func agentIDOf(info *schema.SessionInfo) string {
	if info.Agent == nil {
		return ""
	}
	return string(*info.Agent)
}

func modelIDOf(info *schema.SessionInfo) string {
	if info.Model == nil {
		return ""
	}
	return string(info.Model.ID)
}
