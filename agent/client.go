package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/mcp"
	"github.com/user/go-agent-sdk/schema"
)

// ProviderConfig selects the model provider and credentials.
type ProviderConfig struct {
	// Type is "openai" (default), "anthropic", or "openai-compatible".
	Type    string
	BaseURL string
	APIKey  string
	// Provider is the provider id for openai-compatible endpoints; it defaults
	// to "openai-compatible".
	Provider string
}

// ClientOptions configures a Client.
type ClientOptions struct {
	Provider ProviderConfig
	// Model is the default model id used when a session does not override it.
	Model string
	// WorkingDirectory is the project/worktree root. Defaults to the process cwd.
	WorkingDirectory string
	// DataDir stores the session database. Empty uses the default opencode path.
	DataDir string
	// Tools are application-level tools exposed to every session.
	Tools []Tool
	// MCP configures MCP servers whose tools are registered as application tools.
	MCP []MCPConfig
	// Plugins are loaded at startup and can transform agents and skills.
	Plugins []PluginConfig
	// Permission is the default permission handler. Defaults to ApproveAll.
	Permission PermissionHandlerFunc

	// LLM overrides the default LLM client (route.LLMClient). When nil, a client
	// is built from Provider/Model over the default HTTP transport.
	LLM route.LLMClient
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
	// Mode is "primary", "subagent", or "all" (default).
	Mode        string
	Hidden      bool
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
			if cfg.Mode != "" {
				info.Mode = cfg.Mode
			} else {
				info.Mode = schema.AgentModePrimary
			}
			info.Hidden = cfg.Hidden
			if cfg.Steps > 0 {
				steps := schema.PositiveInt(cfg.Steps)
				info.Steps = &steps
			}
			if cfg.Model != "" {
				info.Model = &schema.ModelRef{ID: schema.ModelID(cfg.Model)}
			}
			if cfg.Permissions != nil {
				info.Permissions = cfg.Permissions
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

	sessionRegistry, err := c.rt.newSessionRegistry(cfg.Tools)
	if err != nil {
		c.removeDerivedAgent(effectiveAgent, derivedAgent)
		return nil, err
	}

	providerID := c.opts.Provider.Type
	if providerID == "" {
		providerID = "openai"
	}
	now := time.Now().UnixMilli()
	title := cfg.Title
	if title == "" {
		title = "Session"
	}
	if _, err := c.rt.db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`revert`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?)",
		string(sessionID), string(c.rt.projectID), "session", string(c.rt.location.Directory),
		title, "1", effectiveAgent, fmt.Sprintf(`{"id":%q,"providerID":%q}`, modelID, providerID), `null`, now, now,
	); err != nil {
		c.removeDerivedAgent(effectiveAgent, derivedAgent)
		return nil, err
	}
	c.rt.sessions.Set(string(sessionID), effectiveAgent)
	c.rt.dispatcher.set(string(sessionID), sessionRegistry)
	c.rt.setHooks(string(sessionID), cfg.Hooks)
	c.rt.permission.setHandler(string(sessionID), cfg.OnPermissionRequest)

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

// Close closes active sessions and releases the runtime and its database.
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
	registry, err := c.rt.newSessionRegistry(cfg.Tools)
	if err != nil {
		return nil, err
	}
	c.rt.dispatcher.set(sessionID, registry)
	c.rt.setHooks(sessionID, cfg.Hooks)
	c.rt.permission.setHandler(sessionID, cfg.OnPermissionRequest)

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
