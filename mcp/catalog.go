package mcp

import (
	"context"
	"regexp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var sanitizePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// Sanitize mirrors McpCatalog.sanitize.
func Sanitize(value string) string {
	return sanitizePattern.ReplaceAllString(value, "_")
}

// ToolName mirrors McpCatalog.toolName: "<sanitized server>_<sanitized name>".
func ToolName(server, name string) string {
	return Sanitize(server) + "_" + Sanitize(name)
}

// ListTools mirrors McpCatalog.listTools. The SDK iterator handles pagination.
// TS also retries tools/list with a tolerant schema when the MCP SDK rejects a
// server's outputSchema; the Go SDK keeps Tool.OutputSchema as `any` and does
// not validate it, so no fallback is needed here.
func ListTools(ctx context.Context, session *sdk.ClientSession) ([]*sdk.Tool, error) {
	var tools []*sdk.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// serverCapabilities returns the server capabilities reported at initialize
// time, or nil when the session has not initialized.
func serverCapabilities(session *sdk.ClientSession) *sdk.ServerCapabilities {
	initialized := session.InitializeResult()
	if initialized == nil {
		return nil
	}
	return initialized.Capabilities
}

// serverSupportsTools mirrors `client.getServerCapabilities()?.tools`, the guard
// index.ts applies before calling McpCatalog.defs.
func serverSupportsTools(session *sdk.ClientSession) bool {
	caps := serverCapabilities(session)
	return caps != nil && caps.Tools != nil
}

// ListPrompts mirrors McpCatalog.prompts: servers that do not advertise the
// prompts capability are not queried and yield no prompts.
func ListPrompts(ctx context.Context, session *sdk.ClientSession) ([]*sdk.Prompt, error) {
	caps := serverCapabilities(session)
	if caps == nil || caps.Prompts == nil {
		return nil, nil
	}
	var prompts []*sdk.Prompt
	for prompt, err := range session.Prompts(ctx, nil) {
		if err != nil {
			return nil, err
		}
		prompts = append(prompts, prompt)
	}
	return prompts, nil
}

// ListResources mirrors McpCatalog.resources: servers that do not advertise the
// resources capability are not queried and yield no resources.
func ListResources(ctx context.Context, session *sdk.ClientSession) ([]*sdk.Resource, error) {
	caps := serverCapabilities(session)
	if caps == nil || caps.Resources == nil {
		return nil, nil
	}
	var resources []*sdk.Resource
	for resource, err := range session.Resources(ctx, nil) {
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

// ListResourceTemplates mirrors McpCatalog.resourceTemplates: servers that do
// not advertise the resources capability are not queried.
func ListResourceTemplates(ctx context.Context, session *sdk.ClientSession) ([]*sdk.ResourceTemplate, error) {
	caps := serverCapabilities(session)
	if caps == nil || caps.Resources == nil {
		return nil, nil
	}
	var templates []*sdk.ResourceTemplate
	for template, err := range session.ResourceTemplates(ctx, nil) {
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}
