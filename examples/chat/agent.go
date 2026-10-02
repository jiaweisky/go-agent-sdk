package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/schema"
)

// agentCommand handles "/agent" (list), "/agent <id>" (switch) and
// "/agent add <name> [<description>]" (register). Switching changes the active
// session's agent in place, so the choice applies from the next turn without
// starting a new session.
func (c *chat) agentCommand(input string) {
	arg := commandArgument(input, "/agent")
	if arg == "add" || strings.HasPrefix(arg, "add ") {
		c.agentAddCommand(strings.TrimSpace(strings.TrimPrefix(arg, "add")))
		return
	}
	name := arg
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

// agentAddCommand registers a runtime agent with Client.RegisterAgent. The
// agent is a hidden subagent with a small step budget, so it shows up in
// /agents (which lists subagents) and can be delegated to with the task tool.
func (c *chat) agentAddCommand(rest string) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		c.console.write("usage: /agent add <name> [<description>]\n")
		return
	}
	name := fields[0]
	if c.client.Agents().Get(schema.AgentID(name)) != nil {
		c.console.write(fmt.Sprintf("agent %s already exists (pick another name)\n", name))
		return
	}
	description := strings.Join(fields[1:], " ")
	if description == "" {
		description = "A runtime-registered helper (registered with /agent add)."
	}
	mode := "subagent"
	hidden := true
	steps := 2
	if err := c.client.RegisterAgent(name, agent.AgentConfig{
		System:      fmt.Sprintf("You are the %s subagent. Answer concisely and stay within your step budget.", name),
		Description: description,
		Mode:        &mode,
		Hidden:      &hidden,
		Steps:       steps,
		Permissions: schema.PermissionRuleset{{Action: "*", Resource: "*", Effect: schema.PermissionEffectAllow}},
	}); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("registered agent %s%s%s (subagent, hidden, steps=%d)\n", cyan, name, reset, steps))
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
