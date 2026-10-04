package agent

import (
	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/tool"
)

// PluginContext is the capability surface a plugin receives. It
// extends the low-level core.PluginContext (agents, skills, references, the
// plugin manager and the raw event log) with per-plugin options and runtime
// tool registration.
type PluginContext struct {
	core.PluginContext
	// Options is the plugin's configuration from PluginConfig.Options.
	Options map[string]any
	// Tools registers application tools for every session, including live ones.
	Tools *ToolRegistrar

	onPanic func(PanicInfo)
}

// PluginFunc loads a plugin. It returns an optional cleanup function invoked
// when the plugin is removed or reloaded.
type PluginFunc func(ctx *PluginContext) (cleanup func(), err error)

// On subscribes to high-level events across every session, including subagent
// child sessions. The returned function unsubscribes.
func (c *PluginContext) On(handler func(Event)) func() {
	return listenEvents(c.Events, handler, c.onPanic)
}

// ToolRegistrar registers application tools at runtime.
type ToolRegistrar struct{ runtime *runtime }

// Register adds tools for sessions created afterwards and for live sessions.
// The returned function unregisters them.
func (t *ToolRegistrar) Register(tools ...Tool) (func(), error) {
	coreTools := make(map[string]tool.Tool, len(tools))
	for _, item := range tools {
		coreTools[item.Name] = item.coreTool(t.runtime.permission)
	}
	return t.runtime.registerTools(coreTools)
}
