package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/schema"
)

// ProviderConfig selects the model provider and credentials.
type ProviderConfig struct {
	// Type is "openai" (default) or "anthropic".
	Type    string
	BaseURL string
	APIKey  string
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
	// Permission is the default permission handler. Defaults to ApproveAll.
	Permission PermissionHandlerFunc

	// llm overrides the LLM client (tests only).
	llm route.LLMClient
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
}

// NewClient builds a client and its backing runtime.
func NewClient(opts *ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &ClientOptions{}
	}
	rt, err := newRuntime(opts)
	if err != nil {
		return nil, err
	}
	client := &Client{opts: *opts, rt: rt}
	return client, nil
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
	if cfg.SystemPrompt != nil {
		if _, err := c.rt.agents.Transform(func(draft core.AgentDraft) error {
			draft.Update(schema.AgentID(agentID), func(info *schema.AgentInfo) {
				applySystemPrompt(info, cfg.SystemPrompt)
			})
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sessionRegistry, err := c.rt.newSessionRegistry(cfg.Tools)
	if err != nil {
		return nil, err
	}

	sessionID := schema.NewSessionID()
	modelID := cfg.Model
	if modelID == "" {
		modelID = c.opts.Model
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
		title, "1", agentID, fmt.Sprintf(`{"id":%q,"providerID":%q}`, modelID, providerID), `null`, now, now,
	); err != nil {
		return nil, err
	}
	c.rt.sessions.Set(string(sessionID), agentID)
	c.rt.dispatcher.set(string(sessionID), sessionRegistry)
	c.rt.setHooks(string(sessionID), cfg.Hooks)

	session := &Session{client: c, id: sessionID, hooks: cfg.Hooks, listeners: map[int]func(Event){}}
	unsub, err := c.rt.events.Listen(func(payload schema.EventPayload) error {
		session.dispatch(payload)
		return nil
	})
	if err != nil {
		return nil, err
	}
	session.unsub = unsub

	if cfg.Hooks != nil && cfg.Hooks.OnSessionStart != nil {
		_, _ = cfg.Hooks.OnSessionStart(SessionStartInput{SessionID: string(sessionID), Agent: agentID, Model: modelID})
	}
	return session, nil
}

// Close releases the runtime and its database.
func (c *Client) Close() error {
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

	session := &Session{client: c, id: schema.SessionID(sessionID), hooks: cfg.Hooks, listeners: map[int]func(Event){}}
	unsubscribe, err := c.rt.events.Listen(func(payload schema.EventPayload) error {
		session.dispatch(payload)
		return nil
	})
	if err != nil {
		return nil, err
	}
	session.unsub = unsubscribe
	if cfg.Hooks != nil && cfg.Hooks.OnSessionStart != nil {
		_, _ = cfg.Hooks.OnSessionStart(SessionStartInput{SessionID: sessionID, Agent: string(agentIDOf(stored)), Model: modelIDOf(stored)})
	}
	return session, nil
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
