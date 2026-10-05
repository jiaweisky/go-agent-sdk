package main

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpCommand lists and fetches MCP prompts and resources: "/mcp prompts",
// "/mcp prompt <name> [k=v ...]", "/mcp resources",
// "/mcp resource [<server>] <uri>".
func (c *chat) mcpCommand(input string) {
	client := c.client.MCP()
	if client == nil {
		c.console.write("(no MCP servers configured)\n")
		return
	}
	action, rest := splitWord(commandArgument(input, "/mcp"))
	switch action {
	case "prompts":
		c.writeMCPPrompts(client)
	case "prompt":
		c.writeMCPPrompt(client, rest)
	case "resources":
		c.writeMCPResources(client)
	case "resource":
		c.writeMCPResource(client, rest)
	default:
		c.writeMCPUsage()
	}
}

func (c *chat) writeMCPUsage() {
	c.console.write("usage: /mcp prompts | prompt <name> [k=v ...] | resources | resource [<server>] <uri>\n")
}

// mcpListItem is one row in a /mcp listing.
type mcpListItem struct {
	label       string
	description string
}

// writeMCPList prints a titled list of MCP entries.
func (c *chat) writeMCPList(title string, items []mcpListItem) {
	c.console.write(fmt.Sprintf("%s%s (%d):%s\n", bold, title, len(items), reset))
	for _, item := range items {
		c.console.write(fmt.Sprintf("  %s%s%s %s— %s%s\n", cyan, item.label, reset, dim, item.description, reset))
	}
}

// writeMCPPrompts lists every server's prompts, keyed "<server>:<name>".
func (c *chat) writeMCPPrompts(client *mcp.Client) {
	prompts := client.Prompts(context.Background())
	if len(prompts) == 0 {
		c.console.write("(no MCP prompts)\n")
		return
	}
	items := make([]mcpListItem, 0, len(prompts))
	for _, key := range slices.Sorted(maps.Keys(prompts)) {
		items = append(items, mcpListItem{label: key, description: prompts[key].Description})
	}
	c.writeMCPList("MCP prompts", items)
}

// writeMCPPrompt fetches a prompt by name (or "<server>:<name>") and prints the
// messages the server filled in. A bare name shared by several servers is
// ambiguous, so the key must be given then.
func (c *chat) writeMCPPrompt(client *mcp.Client, rest string) {
	name, argText := splitWord(rest)
	if name == "" {
		c.writeMCPUsage()
		return
	}
	matches := mcpPromptsFor(client, name)
	switch len(matches) {
	case 0:
		c.console.write(fmt.Sprintf("unknown MCP prompt: %s\n", name))
		return
	case 1:
	default:
		c.console.write(fmt.Sprintf("ambiguous MCP prompt %s; specify the server: /mcp prompt <server>:<name> [k=v ...]\n", name))
		return
	}
	entry := matches[0]
	result, err := client.GetPrompt(context.Background(), entry.Client, entry.Name, parseArgs(argText))
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	for _, message := range result.Messages {
		c.writeMCPMessage(message)
	}
}

// writeMCPMessage prints one prompt message (the text content, when present).
func (c *chat) writeMCPMessage(message *sdk.PromptMessage) {
	text, ok := message.Content.(*sdk.TextContent)
	if !ok {
		return
	}
	c.console.write(fmt.Sprintf("%s%s%s\n", dim, message.Role, reset))
	c.console.write(text.Text + "\n")
}

// writeMCPResources lists static resources and resource templates.
func (c *chat) writeMCPResources(client *mcp.Client) {
	ctx := context.Background()
	resources := client.Resources(ctx)
	templates := client.ResourceTemplates(ctx)
	if len(resources) == 0 && len(templates) == 0 {
		c.console.write("(no MCP resources)\n")
		return
	}
	if len(resources) > 0 {
		items := make([]mcpListItem, 0, len(resources))
		for _, key := range slices.Sorted(maps.Keys(resources)) {
			entry := resources[key]
			items = append(items, mcpListItem{label: entry.URI, description: serverLabel(entry.Client, entry.Description)})
		}
		c.writeMCPList("MCP resources", items)
	}
	if len(templates) > 0 {
		items := make([]mcpListItem, 0, len(templates))
		for _, key := range slices.Sorted(maps.Keys(templates)) {
			entry := templates[key]
			items = append(items, mcpListItem{label: entry.URITemplate, description: serverLabel(entry.Client, entry.Description)})
		}
		c.writeMCPList("MCP resource templates", items)
	}
}

// serverLabel prefixes a description with the owning server, so the reader
// knows what to pass to "/mcp resource <server> <uri>".
func serverLabel(server, description string) string {
	if description == "" {
		return "[" + server + "]"
	}
	return "[" + server + "] " + description
}

// writeMCPResource reads a resource and prints its text contents. The server is
// optional ("/mcp resource <uri>"): it is inferred from an exact resource or a
// unique matching template, and must be given when that is ambiguous.
func (c *chat) writeMCPResource(client *mcp.Client, rest string) {
	first, second := splitWord(rest)
	server, uri := first, second
	if uri == "" {
		server, uri = "", first // a single argument is the URI
	}
	if uri == "" {
		c.writeMCPUsage()
		return
	}
	if server == "" {
		servers := mcpResourceServers(client, uri)
		switch len(servers) {
		case 0:
			c.console.write(fmt.Sprintf("unknown MCP resource: %s\n", uri))
			return
		case 1:
			server = servers[0]
		default:
			c.console.write(fmt.Sprintf("ambiguous MCP resource %s; specify the server: /mcp resource <server> %s\n", uri, uri))
			return
		}
	}
	result, err := client.ReadResource(context.Background(), server, uri)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	for _, content := range result.Contents {
		if content.Text != "" {
			c.console.write(content.Text + "\n")
		}
	}
}

// mcpPromptsFor returns, deterministically, the prompts matching a key
// ("server:name") or a bare name.
func mcpPromptsFor(client *mcp.Client, name string) []mcp.PromptInfo {
	prompts := client.Prompts(context.Background())
	var matches []mcp.PromptInfo
	for _, key := range slices.Sorted(maps.Keys(prompts)) {
		if key == name || prompts[key].Name == name {
			matches = append(matches, prompts[key])
		}
	}
	return matches
}

// mcpResourceServers returns, deterministically, the servers that can serve
// uri: an exact resource, or a template matching the whole URI.
func mcpResourceServers(client *mcp.Client, uri string) []string {
	ctx := context.Background()
	seen := map[string]bool{}
	var servers []string
	add := func(server string) {
		if !seen[server] {
			seen[server] = true
			servers = append(servers, server)
		}
	}
	for _, entry := range client.Resources(ctx) {
		if entry.URI == uri {
			add(entry.Client)
		}
	}
	for _, entry := range client.ResourceTemplates(ctx) {
		if matchURITemplate(entry.URITemplate, uri) {
			add(entry.Client)
		}
	}
	slices.Sort(servers)
	return servers
}

// matchURITemplate reports whether uri is an instance of an RFC 6570 template.
// The whole string is anchored, and each expression is matched as one path
// segment (no "/", "?" or "#"). A template needing a wider expansion simply
// does not match, so the caller asks for an explicit server instead of guessing.
func matchURITemplate(template, uri string) bool {
	var pattern strings.Builder
	pattern.WriteByte('^')
	for template != "" {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			pattern.WriteString(regexp.QuoteMeta(template))
			break
		}
		pattern.WriteString(regexp.QuoteMeta(template[:open]))
		closeIdx := strings.IndexByte(template[open:], '}')
		if closeIdx < 0 {
			pattern.WriteString(regexp.QuoteMeta(template[open:]))
			break
		}
		pattern.WriteString("[^/?#]+")
		template = template[open+closeIdx+1:]
	}
	pattern.WriteByte('$')
	return regexp.MustCompile(pattern.String()).MatchString(uri)
}

// parseArgs parses "k=v k2=v2" into a map, ignoring malformed fields.
func parseArgs(text string) map[string]string {
	if text == "" {
		return nil
	}
	args := map[string]string{}
	for _, field := range strings.Fields(text) {
		if key, value, ok := strings.Cut(field, "="); ok {
			args[key] = value
		}
	}
	return args
}
