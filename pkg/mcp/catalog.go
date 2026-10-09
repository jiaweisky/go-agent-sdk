package mcp

import (
	"context"
	"regexp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var sanitizePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// Sanitize replaces every character a tool name cannot contain with "_".
func Sanitize(value string) string {
	return sanitizePattern.ReplaceAllString(value, "_")
}

// ToolName is "<sanitized server>_<sanitized tool>".
func ToolName(server, name string) string {
	return Sanitize(server) + "_" + Sanitize(name)
}

// ListTools lists a server's tools; a server without the tools capability
// yields none. The SDK iterator handles pagination, and the tool output schema
// is passed through unvalidated.
func ListTools(ctx context.Context, session *sdk.ClientSession) ([]*sdk.Tool, error) {
	caps := serverCapabilities(session)
	if caps == nil || caps.Tools == nil {
		return nil, nil
	}
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

// ListPrompts lists a server's prompts; a server without the prompts capability
// yields none.
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

// ListResources lists a server's resources; a server without the resources
// capability yields none.
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

// ListResourceTemplates lists a server's resource templates; a server without the
// resources capability yields none.
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
