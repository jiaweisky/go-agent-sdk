package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/user/go-agent-sdk"
	"github.com/user/go-agent-sdk/core/util"
	"github.com/user/go-agent-sdk/mcp"
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
	instructions := map[string]string{}
	for _, block := range client.Instructions() {
		instructions[block.Name] = block.Instructions
	}
	names := sortedServerNames(statuses)
	c.console.write(fmt.Sprintf("%sMCP servers (%d):%s\n", bold, len(names), reset))
	for _, name := range names {
		c.writeMCPServer(client, name, statuses[name], instructions[name])
	}
}

// sortedServerNames returns the connected server names in locale order.
func sortedServerNames(statuses map[string]mcp.Status) []string {
	names := make([]string, 0, len(statuses))
	for name := range statuses {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return util.CompareLocale(names[i], names[j], util.LocaleTag) < 0 })
	return names
}

// writeMCPServer prints one server: its status line, metadata and instructions.
func (c *chat) writeMCPServer(client *mcp.Client, name string, status mcp.Status, instructions string) {
	line := fmt.Sprintf("  %s%s%s — %s", cyan, name, reset, status.Status)
	if status.Error != "" {
		line += fmt.Sprintf(" %s(%s)%s", dim, status.Error, reset)
	}
	c.console.write(line + "\n")

	if info, ok := client.ServerInfo(name); ok {
		if meta := serverMeta(info); meta != "" {
			c.console.write(fmt.Sprintf("    %s%s%s\n", dim, meta, reset))
		}
		if info.Description != "" {
			c.console.write(fmt.Sprintf("    %s\n", info.Description))
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(instructions), "\n") {
		if line != "" {
			c.console.write(fmt.Sprintf("    %s%s%s\n", dim, line, reset))
		}
	}
}

// serverMeta renders a server's "Title Name Version" metadata, dropping the
// parts it does not report.
func serverMeta(info mcp.ServerMetadata) string {
	meta := info.Title
	if meta == "" {
		meta = info.Name
	}
	if info.Version == "" {
		return meta
	}
	if meta == "" {
		return info.Version
	}
	return meta + " " + info.Version
}

// helpCommand lists the slash commands. It mirrors the startup banner but adds
// each command's arguments, aliases and a one-line description.
func (c *chat) helpCommand() {
	c.console.write(fmt.Sprintf("%sCommands:%s\n", bold, reset))
	for _, entry := range helpCommands {
		c.console.write(fmt.Sprintf("  %s%s%s — %s\n", cyan, entry.usage, reset, entry.summary))
	}
}

// helpEntry is one row of the /help listing.
type helpEntry struct {
	usage   string
	summary string
}

// helpCommands is the single source of truth for the available commands. Keep
// it in sync with handleCommand.
var helpCommands = []helpEntry{
	{"/new, /clear", "start a new session"},
	{"/sessions [<#n|id>]", "list sessions, or switch to one (/resume, /continue)"},
	{"/delete <#n|id>", "delete a session"},
	{"/model [<id>]", "list models, or switch the current session's model"},
	{"/agent [<id> | add <name>]", "list/switch the session agent, or register one"},
	{"/skills", "list discovered skills"},
	{"/agents", "list subagents"},
	{"/mcps", "list MCP servers"},
	{"/mcp prompts|prompt|resources|resource", "list or fetch MCP prompts and resources"},
	{"/task [<#n>]", "list subagent tasks, or show one's output"},
	{"/compact", "summarize the history now"},
	{"/tool [add|remove <name>]", "list or manage runtime tools"},
	{"/reference [add <name> <path> | add-git <name> <repo> [<branch>] | remove <name>]", "list or manage reference sources"},
	{"/persona [--replace] <text>", "start a session with an appended or replaced system prompt"},
	{"/prompt add|replace|remove <section> [<text>]", "start a session with a customized prompt section"},
	{"/history", "show the session's stored messages"},
	{"/help", "show this help"},
}

// compactCommand summarizes the session history on demand via Session.Compact.
// The summary itself is rendered from the compaction events, so this only
// reports failures and the no-op case.
func compactCommand(c *chat) {
	if c.session == nil {
		c.console.write("(no active session)\n")
		return
	}
	compacted, err := c.session.Compact(context.Background())
	switch {
	case err != nil:
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
	case !compacted:
		c.console.write("(nothing to compact; the history is already short)\n")
	}
}
