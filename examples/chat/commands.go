package main

import (
	"fmt"
	"sort"

	"github.com/user/go-agent-sdk/agent"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/schema"
)

// skillsCommand lists the skills discovered from the config directories
// (<configDir>/skill or <configDir>/skills).
func (c *chat) skillsCommand() {
	skills, err := c.client.Skills().List()
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	if len(skills) == 0 {
		c.console.write("(no skills; add <configDir>/skill/<name>/SKILL.md)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sSkills (%d):%s\n", bold, len(skills), reset))
	for _, skill := range skills {
		c.console.write(fmt.Sprintf("  %s%s%s — %s\n", cyan, skill.Name, reset, describe(skill.Description)))
		c.console.write(fmt.Sprintf("    %s%s%s\n", dim, skill.Location, reset))
	}
}

// describe renders a skill's or agent's description, or a placeholder when it
// has none.
func describe(description *string) string {
	if description != nil && *description != "" {
		return *description
	}
	return "(no description)"
}

// agentsCommand lists the subagents the session's agent can delegate to
// (non-primary agents whose "task" permission is not denied).
func (c *chat) agentsCommand() {
	agents := c.client.Agents()
	var permissions schema.PermissionRuleset
	if info := agents.Get(schema.AgentID(agentName)); info != nil {
		permissions = info.Permissions
	}
	subagents := agent.SubagentTypes(agents, permissions, util.LocaleTag)
	if len(subagents) == 0 {
		c.console.write("(no subagents; add <configDir>/agent/<name>.md)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sSubagents (%d):%s\n", bold, len(subagents), reset))
	for _, item := range subagents {
		c.console.write(fmt.Sprintf("  %s%s%s — %s\n", cyan, item.ID, reset, describe(item.Description)))
	}
}

// mcpsCommand lists the configured MCP servers and their connection status.
func (c *chat) mcpsCommand() {
	client := c.client.MCP()
	if client == nil {
		c.console.write("(no MCP servers configured)\n")
		return
	}
	statuses := client.Status()
	if len(statuses) == 0 {
		c.console.write("(no MCP servers connected)\n")
		return
	}
	names := make([]string, 0, len(statuses))
	for name := range statuses {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return util.CompareLocale(names[i], names[j], util.LocaleTag) < 0 })
	c.console.write(fmt.Sprintf("%sMCP servers (%d):%s\n", bold, len(names), reset))
	for _, name := range names {
		status := statuses[name]
		line := fmt.Sprintf("  %s%s%s — %s", cyan, name, reset, status.Status)
		if status.Error != "" {
			line += fmt.Sprintf(" %s(%s)%s", dim, status.Error, reset)
		}
		c.console.write(line + "\n")

		info, ok := client.ServerInfo(name)
		if !ok {
			continue
		}
		meta := info.Title
		if meta == "" {
			meta = info.Name
		}
		if info.Version != "" {
			if meta != "" {
				meta += " " + info.Version
			} else {
				meta = info.Version
			}
		}
		if meta != "" {
			c.console.write(fmt.Sprintf("    %s%s%s\n", dim, meta, reset))
		}
		if info.Description != "" {
			c.console.write(fmt.Sprintf("    %s\n", info.Description))
		}
	}
}
