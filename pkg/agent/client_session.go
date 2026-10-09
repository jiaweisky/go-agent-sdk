package agent

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// SessionInfo summarizes a stored session.
type SessionInfo struct {
	ID        string
	Title     string
	Agent     string
	Directory string
	// ParentID is the parent session id for a subagent (task) child session,
	// or empty for a top-level session.
	ParentID    string
	TimeCreated int64
	TimeUpdated int64
}

// ResumeSession reattaches to an existing session. The context is accepted for
// API symmetry; resuming is synchronous and does not currently observe
// cancellation.
func (c *Client) ResumeSession(ctx context.Context, sessionID string, cfg *SessionConfig) (*Session, error) {
	if cfg == nil {
		cfg = &SessionConfig{}
	}
	id := schema.SessionID(sessionID)
	stored, err := c.rt.store.Get(id)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	sessionRules, err := c.overrideStoredPermissions(sessionID, stored, cfg.Permissions)
	if err != nil {
		return nil, err
	}
	// The stored agent is the durable base id; re-derive a session agent when
	// the resume config customizes the system prompt.
	agentID := baseAgentID(agentIDOf(stored))
	if agentID == "" {
		agentID = string(core.AgentDefaultID)
	}
	effectiveAgent, derivedAgent, err := c.deriveAgent(cfg, id, agentID, modelIDOf(stored))
	if err != nil {
		return nil, err
	}
	registry, err := c.rt.newSessionRegistry(effectiveAgent, sessionRules, cfg.Tools)
	if err != nil {
		return nil, err
	}
	return c.newSession(id, effectiveAgent, derivedAgent, registry, cfg)
}

// overrideStoredPermissions returns the ruleset a session resumes with. A
// per-session ruleset passed on resume replaces the stored one.
func (c *Client) overrideStoredPermissions(
	sessionID string,
	stored *schema.SessionInfo,
	rules schema.PermissionRuleset,
) (schema.PermissionRuleset, error) {
	if len(rules) == 0 {
		return permissionRulesOf(stored), nil
	}
	encoded, err := encodePermissionRules(rules)
	if err != nil {
		return nil, err
	}
	if _, err := c.rt.db.DB.Exec("UPDATE `session` SET `permission` = ? WHERE `id` = ?", encoded, sessionID); err != nil {
		return nil, err
	}
	return rules, nil
}

// trackSession reserves the session, failing when its id is already attached.
func (c *Client) trackSession(session *Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions == nil {
		c.sessions = map[string]*Session{}
	}
	id := session.ID()
	if _, ok := c.sessions[id]; ok {
		return fmt.Errorf("session already attached: %s", id)
	}
	c.sessions[id] = session
	return nil
}

func (c *Client) untrackSession(session *Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, session.ID())
}

// sessionByID returns the attached session with the given id, or nil.
func (c *Client) sessionByID(id string) *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

// SessionMessages returns the stored messages of any session, including
// subagent child sessions. It is the active context (messages after the last
// compaction); use SessionHistory for the full transcript.
func (c *Client) SessionMessages(sessionID string) ([]schema.SessionMessage, error) {
	return c.rt.store.Context(schema.SessionID(sessionID))
}

// SessionHistory returns every stored message of any session, ignoring
// compaction and the context-epoch baseline (the full transcript).
func (c *Client) SessionHistory(sessionID string) ([]schema.SessionMessage, error) {
	return c.rt.store.Messages(schema.SessionID(sessionID))
}

// ListSessions returns the sessions in the client's project, newest first,
// including subagent (task) child sessions; use SessionInfo.ParentID to tell
// the two apart. The context is accepted for API symmetry; the query is
// synchronous.
func (c *Client) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	rows, err := c.rt.db.DB.Query(
		"SELECT `id`,`title`,`agent`,`directory`,`parent_id`,`time_created`,`time_updated` FROM `session` WHERE `project_id` = ? ORDER BY `time_updated` DESC",
		string(c.rt.projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SessionInfo
	for rows.Next() {
		var info SessionInfo
		var agent, parentID *string
		if err := rows.Scan(
			&info.ID,
			&info.Title,
			&agent,
			&info.Directory,
			&parentID,
			&info.TimeCreated,
			&info.TimeUpdated,
		); err != nil {
			return nil, err
		}
		if agent != nil {
			info.Agent = *agent
		}
		if parentID != nil {
			info.ParentID = *parentID
		}
		result = append(result, info)
	}
	return result, rows.Err()
}

// DeleteSession permanently removes a session, its subagent (child) sessions and
// their durable event logs, closing any attached live handles first. The context
// is accepted for API symmetry; the delete is synchronous.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	ids, err := c.sessionSubtree(sessionID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if session := c.sessionByID(id); session != nil {
			_ = session.Close()
		}
		c.rt.detach(id)
		// The event log has no foreign key to the session, so it survives the
		// row delete (and would leak the whole conversation) unless removed.
		if err := c.rt.events.Remove(id); err != nil {
			return err
		}
	}
	return deleteSessions(c.rt.db.DB, ids)
}

// sessionSubtree returns sessionID and every descendant session id (the task
// subagent sessions it started), so a delete covers the whole tree.
func (c *Client) sessionSubtree(sessionID string) ([]string, error) {
	rows, err := c.rt.db.DB.Query(
		"WITH RECURSIVE subtree(`id`) AS ("+
			"SELECT `id` FROM `session` WHERE `id` = ? "+
			"UNION ALL "+
			"SELECT `s`.`id` FROM `session` `s` JOIN subtree `t` ON `s`.`parent_id` = `t`.`id`"+
			") SELECT `id` FROM subtree",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// deleteSessions removes the rows, cascading to each session's messages, inputs,
// context epochs and todos.
func deleteSessions(db *sql.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := db.Exec("DELETE FROM `session` WHERE `id` IN ("+placeholders+")", args...)
	return err
}

// sessionRow is the durable field set a new session row is written with.
type sessionRow struct {
	ID         schema.SessionID
	ProjectID  schema.ProjectID
	ParentID   *string
	Directory  string
	Title      string
	Agent      string
	Model      string
	Permission string
	// Revert is the stored revert state: the JSON literal "null" for a fresh
	// top-level session, nil for a child session.
	Revert *string
}

// insertSessionRow writes one session row. It always supplies parent_id and
// revert so top-level and child inserts share the same statement.
func insertSessionRow(db *sql.DB, row sessionRow) error {
	now := time.Now().UnixMilli()
	_, err := db.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`parent_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`permission`,`revert`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
		string(row.ID),
		string(row.ProjectID),
		row.ParentID,
		"session",
		row.Directory,
		row.Title,
		"1",
		row.Agent,
		row.Model,
		row.Permission,
		row.Revert,
		now,
		now,
	)
	return err
}

// insertSession records the session row.
func (c *Client) insertSession(cfg *SessionConfig, sessionID schema.SessionID, agentID, modelID string) error {
	// The stored provider id is informational; resolution uses the resolved
	// ModelInfo. It is the configured protocol (empty when ModelInfo is set).
	providerID := string(c.opts.Protocol)
	title := cfg.Title
	if title == "" {
		title = defaultSessionTitle(time.Now())
	}
	permissionJSON, err := encodePermissionRules(cfg.Permissions)
	if err != nil {
		return err
	}
	revert := "null"
	return insertSessionRow(c.rt.db.DB, sessionRow{
		ID:         sessionID,
		ProjectID:  c.rt.projectID,
		Directory:  string(c.rt.location.Directory),
		Title:      title,
		Agent:      agentID,
		Model:      modelRefJSON(schema.ModelID(modelID), schema.ProviderID(providerID)),
		Permission: permissionJSON,
		Revert:     &revert,
	})
}

// defaultSessionTitle is the placeholder title a fresh session carries until
// the title agent replaces it. Its shape (an ISO-8601 UTC timestamp with
// millisecond precision) lets IsDefaultTitle recognize it.
func defaultSessionTitle(now time.Time) string {
	return "New session - " + now.UTC().Format("2006-01-02T15:04:05.000Z")
}

func agentIDOf(info *schema.SessionInfo) string {
	if info.Agent == nil {
		return ""
	}
	return string(*info.Agent)
}

// baseAgentID strips the session-scoped suffix a legacy row may carry, so
// resume always starts from the durable agent id.
func baseAgentID(agentID string) string {
	base, _, _ := strings.Cut(agentID, "#")
	return base
}

func modelIDOf(info *schema.SessionInfo) string {
	if info.Model == nil {
		return ""
	}
	return string(info.Model.ID)
}

// permissionRulesOf returns the ruleset a stored session carries, or nil.
func permissionRulesOf(info *schema.SessionInfo) schema.PermissionRuleset {
	if info.Permission == nil {
		return nil
	}
	return *info.Permission
}

// modelRefJSON renders a model reference as the JSON stored in a session row,
// or "null" when no model id is known.
func modelRefJSON(id schema.ModelID, providerID schema.ProviderID) string {
	if id == "" {
		return "null"
	}
	return fmt.Sprintf(`{"id":%q,"providerID":%q}`, string(id), string(providerID))
}
