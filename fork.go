package agent

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/schema"
)

// forkedTitlePattern matches a title that already carries a "(fork #N)" suffix.
var forkedTitlePattern = regexp.MustCompile(`^(.+) \(fork #(\d+)\)$`)

// forkedTitle appends "(fork #1)" to a title, or bumps an existing suffix.
func forkedTitle(title string) string {
	if match := forkedTitlePattern.FindStringSubmatch(title); match != nil {
		number, _ := strconv.Atoi(match[2])
		return fmt.Sprintf("%s (fork #%d)", match[1], number+1)
	}
	return title + " (fork #1)"
}

// ForkSession creates a new session that copies sessionID's history up to
// messageID and returns it, leaving the source untouched. An empty messageID
// copies the whole history. The fork keeps the source's agent and model and gets
// a "(fork #N)" title. Each copied message keeps its content and timestamps but
// gets a fresh message id.
func (c *Client) ForkSession(ctx context.Context, sessionID, messageID string) (*Session, error) {
	stored, err := c.rt.store.Get(schema.SessionID(sessionID))
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	cfg := &SessionConfig{Agent: baseAgentID(agentIDOf(stored)), Title: forkedTitle(stored.Title)}
	if stored.Model != nil {
		cfg.Model = string(stored.Model.ID)
	}
	forked, err := c.CreateSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := session.Fork(c.rt.db, schema.SessionID(sessionID), schema.SessionID(forked.ID()), schema.MessageID(messageID)); err != nil {
		_ = forked.Close()
		_ = c.DeleteSession(ctx, forked.ID())
		return nil, err
	}
	return forked, nil
}
