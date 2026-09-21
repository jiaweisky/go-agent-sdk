package schema

// MCP event definitions mirror packages/schema/src/mcp-event.ts.
var McpEventToolsChanged = Define(DefineInput{
	Type:   "mcp.tools.changed",
	Schema: map[string]any{"server": nil},
})

var McpEventBrowserOpenFailed = Define(DefineInput{
	Type:   "mcp.browser.open.failed",
	Schema: map[string]any{"mcpName": nil, "url": nil},
})

var McpEvent = struct {
	ToolsChanged      EventDefinition
	BrowserOpenFailed EventDefinition
	Definitions       []EventDefinition
}{
	ToolsChanged:      McpEventToolsChanged,
	BrowserOpenFailed: McpEventBrowserOpenFailed,
	Definitions:       Inventory(McpEventToolsChanged, McpEventBrowserOpenFailed),
}
