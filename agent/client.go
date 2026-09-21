package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/tool"
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
	for _, userTool := range opts.Tools {
		if _, err := rt.tools.Register(map[string]tool.Tool{userTool.Name: userTool.build(rt.permission)}); err != nil {
			_ = rt.close()
			return nil, err
		}
	}
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
	for _, userTool := range cfg.Tools {
		if _, err := c.rt.tools.Register(map[string]tool.Tool{userTool.Name: userTool.build(c.rt.permission)}); err != nil {
			return nil, err
		}
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
