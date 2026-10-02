package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/schema"
)

// agentCommand handles "/agent" (list) and "/agent <id>" (switch). It switches
// the active session's agent in place, so the choice applies from the next turn
// without starting a new session.
func (c *chat) agentCommand(input string) {
	name := commandArgument(input, "/agent")
	if name == "" {
		c.writeAgents()
		return
	}
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	info := c.client.Agents().Get(schema.AgentID(name))
	if info == nil {
		c.console.write(fmt.Sprintf("unknown agent: %s\n", name))
		return
	}
	if !isPrimaryAgent(info) {
		c.console.write(fmt.Sprintf("%s is not a primary agent\n", name))
		return
	}
	if err := c.session.SetAgent(name); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.state.Agent = name
	c.persist()
	c.console.write(fmt.Sprintf("agent set to %s%s%s\n", cyan, name, reset))
}

// isPrimaryAgent reports whether an agent can be selected as a session's primary
// agent: it must exist, not be hidden, and not be a subagent.
func isPrimaryAgent(info *schema.AgentInfo) bool {
	return info != nil && !info.Hidden && info.Mode != schema.AgentModeSubagent
}

// defaultAgentName is the agent new sessions should use: the persisted choice
// when set, otherwise the build agent.
func (c *chat) defaultAgentName() string {
	if c.state.Agent != "" {
		return c.state.Agent
	}
	return agentName
}

// resolveDefaultAgent returns the persisted agent when it is still a selectable
// primary agent, otherwise the build agent.
func resolveDefaultAgent(client *agent.Client, persisted string) string {
	if persisted != "" && isPrimaryAgent(client.Agents().Get(schema.AgentID(persisted))) {
		return persisted
	}
	return agentName
}

// writeAgents lists the selectable primary agents, marking the active session's.
func (c *chat) writeAgents() {
	current := c.sessionAgent()
	agents := c.client.Agents().All()
	names := make([]string, 0, len(agents))
	for i := range agents {
		if isPrimaryAgent(&agents[i]) {
			names = append(names, string(agents[i].ID))
		}
	}
	sort.Strings(names)
	c.console.write(fmt.Sprintf("%sAgents (%d):%s\n", bold, len(names), reset))
	for _, name := range names {
		marker := " "
		if name == current {
			marker = "▶"
		}
		c.console.write(fmt.Sprintf("  %s %s\n", marker, name))
	}
}

// sessionAgent returns the active session's stored agent, or "".
func (c *chat) sessionAgent() string {
	if c.session == nil {
		return ""
	}
	sessions, err := c.client.ListSessions(context.Background())
	if err != nil {
		return ""
	}
	for _, info := range sessions {
		if info.ID == c.session.ID() {
			return info.Agent
		}
	}
	return ""
}
