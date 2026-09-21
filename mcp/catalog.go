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

// ListPrompts mirrors McpCatalog.prompts.
func ListPrompts(ctx context.Context, session *sdk.ClientSession) ([]*sdk.Prompt, error) {
	var prompts []*sdk.Prompt
	for prompt, err := range session.Prompts(ctx, nil) {
		if err != nil {
			return nil, err
		}
		prompts = append(prompts, prompt)
	}
	return prompts, nil
}

// ListResources mirrors McpCatalog.resources.
func ListResources(ctx context.Context, session *sdk.ClientSession) ([]*sdk.Resource, error) {
	var resources []*sdk.Resource
	for resource, err := range session.Resources(ctx, nil) {
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

// ListResourceTemplates mirrors McpCatalog.resourceTemplates.
func ListResourceTemplates(ctx context.Context, session *sdk.ClientSession) ([]*sdk.ResourceTemplate, error) {
	var templates []*sdk.ResourceTemplate
	for template, err := range session.ResourceTemplates(ctx, nil) {
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}
