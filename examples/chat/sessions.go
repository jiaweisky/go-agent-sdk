package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/agent"
)

// newCommand starts a fresh session on the same agent and switches the chat to
// it, closing the previous session. It is the plain counterpart to /persona,
// which starts a session with an appended system prompt.
func (c *chat) newCommand() {
	session, err := c.client.CreateSession(context.Background(), c.sessionConfig())
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.replaceSession(session)
	c.console.write(fmt.Sprintf("new session %s%s%s\n", cyan, session.ID(), reset))
}

// sessionsCommand lists the project's top-level sessions and switches to one
// with "/sessions <#n|id>". It backs /sessions, /resume and /continue.
func (c *chat) sessionsCommand(input string) {
	sessions, err := c.topSessions()
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	arg := commandArgument(input, "/sessions", "/resume", "/continue")
	if arg == "" {
		c.writeSessions(sessions)
		return
	}
	id, ok := resolveSession(sessions, arg)
	if !ok {
		c.console.write("session not found: " + arg + "\n")
		return
	}
	c.resumeCommand(id)
}

// deleteCommand removes the session named by "/delete <#n|id>". Deleting the
// active session first starts a fresh one, so the chat never points at a
// deleted session.
func (c *chat) deleteCommand(input string) {
	arg := commandArgument(input, "/delete")
	if arg == "" {
		c.console.write("usage: /delete <#n|id>  (remove a session)\n")
		return
	}
	sessions, err := c.topSessions()
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	id, ok := resolveSession(sessions, arg)
	if !ok {
		c.console.write("session not found: " + arg + "\n")
		return
	}
	if c.session != nil && c.session.ID() == id {
		session, err := c.client.CreateSession(context.Background(), c.sessionConfig())
		if err != nil {
			c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
			return
		}
		c.replaceSession(session)
	}
	if err := c.client.DeleteSession(context.Background(), id); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("deleted session %s%s%s\n", cyan, id, reset))
}

// resumeCommand reattaches to a stored session and switches the chat to it,
// closing the previous session.
func (c *chat) resumeCommand(id string) {
	if c.session != nil && c.session.ID() == id {
		c.console.write("(already the active session)\n")
		return
	}
	session, err := c.client.ResumeSession(context.Background(), id, c.sessionConfig())
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.replaceSession(session)
	c.console.write(fmt.Sprintf("switched to session %s%s%s\n", cyan, session.ID(), reset))
}

// sessionConfig is the base config for sessions the chat creates or resumes: the
// build agent plus the session hooks.
func (c *chat) sessionConfig() *agent.SessionConfig {
	return &agent.SessionConfig{Agent: c.defaultAgentName(), Hooks: newSessionHooks(c.console)}
}

// replaceSession points the renderer at next and closes the previous live
// handle, keeping at most one session attached (a still-attached session cannot
// be resumed again).
func (c *chat) replaceSession(next *agent.Session) {
	previous := c.session
	c.switchSession(next)
	if previous != nil {
		_ = previous.Close()
	}
}

// topSessions lists the project's sessions without the subagent (task) child
// sessions, which share the project but are not conversations the user drives.
func (c *chat) topSessions() ([]agent.SessionInfo, error) {
	all, err := c.client.ListSessions(context.Background())
	if err != nil {
		return nil, err
	}
	roots := make([]agent.SessionInfo, 0, len(all))
	for _, info := range all {
		if info.ParentID == "" {
			roots = append(roots, info)
		}
	}
	return roots, nil
}

// writeSessions prints the session list, newest first, marking the active one.
func (c *chat) writeSessions(sessions []agent.SessionInfo) {
	if len(sessions) == 0 {
		c.console.write("(no sessions)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sSessions (%d):%s\n", bold, len(sessions), reset))
	for index, info := range sessions {
		marker, style := " ", ""
		if c.session != nil && c.session.ID() == info.ID {
			marker, style = "▶", cyan
		}
		title := info.Title
		if title == "" {
			title = "(untitled)"
		}
		agentLabel := info.Agent
		if agentLabel == "" {
			agentLabel = "?"
		}
		c.console.write(fmt.Sprintf("  %s%s #%d%s %s — %s · %s  %s%s%s\n",
			style, marker, index+1, reset, title, agentLabel,
			time.UnixMilli(info.TimeUpdated).Format("2006-01-02 15:04"),
			dim, info.ID, reset))
	}
}

// resolveSession maps a "#n" index (1-based, in list order) or a full/prefix
// session id to a stored session id.
func resolveSession(sessions []agent.SessionInfo, arg string) (string, bool) {
	arg = strings.TrimPrefix(strings.TrimSpace(arg), "#")
	if index, err := strconv.Atoi(arg); err == nil {
		if index >= 1 && index <= len(sessions) {
			return sessions[index-1].ID, true
		}
		return "", false
	}
	for _, info := range sessions {
		if info.ID == arg || strings.HasPrefix(info.ID, arg) {
			return info.ID, true
		}
	}
	return "", false
}

// commandFields splits a command's arguments into whitespace-separated fields,
// so "/tool add now" yields ["add", "now"].
func commandFields(input, name string) []string {
	return strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, name)))
}

// commandArgument returns the text after a command name, accepting any of the
// given aliases, and "" when the input is the bare command.
func commandArgument(input string, names ...string) string {
	for _, name := range names {
		if input == name {
			return ""
		}
		if strings.HasPrefix(input, name+" ") {
			return strings.TrimSpace(strings.TrimPrefix(input, name))
		}
	}
	return ""
}
