package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/jsonschema"
)

// AdaptMCPTool binds an MCP tool definition to its session and returns a core
// tool.
func AdaptMCPTool(def *sdk.Tool, session *sdk.ClientSession, server string, timeout time.Duration) tool.Tool {
	return tool.Make(tool.Config[map[string]any, any, any]{
		Description: def.Description,
		Input:       jsonschema.Raw(MCPInputSchema(def)),
		Output:      jsonschema.Raw(map[string]any{}),
		Execute: func(ctx context.Context, input map[string]any, tctx tool.Context) (any, error) {
			callCtx := ctx
			if timeout > 0 {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			result, err := session.CallTool(callCtx, &sdk.CallToolParams{Name: def.Name, Arguments: input})
			if err != nil {
				return nil, err
			}
			if result.IsError {
				return nil, fmt.Errorf("%s", mcpErrorText(result))
			}
			return result, nil
		},
		ToModelOutput: func(input map[string]any, output any) []tool.Content {
			return []tool.Content{tool.TextContent{Text: RenderMCPResult(output)}}
		},
	})
}

// MCPInputSchema normalizes a server's input schema: forced to an object with a
// properties map and additionalProperties:false.
func MCPInputSchema(def *sdk.Tool) map[string]any {
	schema := map[string]any{}
	if raw, ok := def.InputSchema.(map[string]any); ok {
		for key, value := range raw {
			schema[key] = value
		}
	}
	schema["type"] = "object"
	if _, ok := schema["properties"]; !ok {
		schema["properties"] = map[string]any{}
	}
	schema["additionalProperties"] = false
	return schema
}

// AdaptedTool is a discovered MCP tool plus the session it belongs to.
type AdaptedTool struct {
	Def     *sdk.Tool
	Session *sdk.ClientSession
	Server  string
	Timeout time.Duration
}

// CoreTool converts the adapted tool into a core tool.
func (t AdaptedTool) CoreTool() tool.Tool {
	return AdaptMCPTool(t.Def, t.Session, t.Server, t.Timeout)
}

func mcpErrorText(result *sdk.CallToolResult) string {
	parts := mcpTextParts(result)
	if len(parts) == 0 {
		return "MCP tool returned an error"
	}
	return strings.Join(parts, "\n\n")
}

// RenderMCPResult renders an MCP call result as text: text content first, then
// structured content as JSON. The core tool framework passes the JSON-encoded
// result (map[string]any), so both shapes are handled.
func RenderMCPResult(output any) string {
	switch value := output.(type) {
	case *sdk.CallToolResult:
		if parts := mcpTextParts(value); len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
		if value.StructuredContent != nil {
			if data, err := json.Marshal(value.StructuredContent); err == nil {
				return string(data)
			}
		}
	case map[string]any:
		var parts []string
		if content, ok := value["content"].([]any); ok {
			for _, item := range content {
				block, ok := item.(map[string]any)
				if !ok || block["type"] != "text" {
					continue
				}
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
		if structured, ok := value["structuredContent"]; ok && structured != nil {
			if data, err := json.Marshal(structured); err == nil {
				return string(data)
			}
		}
	}
	return ""
}

func mcpTextParts(result *sdk.CallToolResult) []string {
	var parts []string
	for _, item := range result.Content {
		text, ok := item.(*sdk.TextContent)
		if !ok || strings.TrimSpace(text.Text) == "" {
			continue
		}
		parts = append(parts, text.Text)
	}
	return parts
}
