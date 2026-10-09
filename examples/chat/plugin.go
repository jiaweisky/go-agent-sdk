package main

import (
	"fmt"
	"sync/atomic"

	"github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const (
	// greeterPluginID is the id of the greeter plugin, used by "/plugin remove".
	greeterPluginID = "chat-greeter"
	// permissionPluginID is the id of the permission plugin.
	permissionPluginID = "chat-permission"
	// pluginGreetTool is the tool the greeter plugin registers.
	pluginGreetTool = "plugin_greet"
)

// pluginLine renders a dim "[plugin] ..." line, matching the other bracketed
// lifecycle lines the chat prints.
func pluginLine(format string, args ...any) string {
	return fmt.Sprintf("%s[plugin] %s%s\n", dim, fmt.Sprintf(format, args...), reset)
}

// examplePlugins is the plugins the chat loads at startup.
func examplePlugins(console *console, yolo bool) []agent.PluginConfig {
	plugins := []agent.PluginConfig{greeterPlugin(console)}
	if !yolo {
		plugins = append(plugins, permissionPlugin())
	}
	return plugins
}

// pluginRegistry tracks the plugins the chat loaded. The SDK's core.Plugin has
// no lookup, so /plugin remove cannot ask it whether an id is active; the chat
// remembers instead. It is only touched from the REPL, so it needs no lock.
type pluginRegistry struct {
	active map[string]bool
}

func newPluginRegistry(plugins []agent.PluginConfig) *pluginRegistry {
	registry := &pluginRegistry{active: make(map[string]bool, len(plugins))}
	for _, config := range plugins {
		registry.active[config.ID] = true
	}
	return registry
}

// has reports whether a plugin id was loaded and not yet removed.
func (r *pluginRegistry) has(id string) bool { return r.active[id] }

func (r *pluginRegistry) remove(id string) { delete(r.active, id) }

// greeterPlugin is a config-driven plugin registered through ClientOptions.Plugins
// (via util.WithPlugins). It reads its greeting from Options, counts tool calls
// with PluginContext.On, and registers an application tool with
// PluginContext.Tools. Its cleanup (run by /plugin remove or client close)
// unsubscribes and unregisters.
func greeterPlugin(console *console) agent.PluginConfig {
	return agent.PluginConfig{
		ID:      greeterPluginID,
		Options: map[string]any{"greeting": "Hello from the plugin"},
		Func: func(ctx *agent.PluginContext) (func(), error) {
			greeting, _ := ctx.Options["greeting"].(string)
			var calls atomic.Int64
			unsubscribe := ctx.On(func(event agent.Event) {
				if _, ok := event.(agent.ToolCalled); ok {
					calls.Add(1)
				}
			})
			tool := agent.DefineTool(pluginGreetTool, "Greet by name, reporting how many tools have run.",
				func(input pluginGreetInput, _ agent.ToolContext) (string, error) {
					return fmt.Sprintf("%s, %s (%d tool call(s) so far)", greeting, input.Name, calls.Load()), nil
				})
			unregister, err := ctx.Tools.Register(tool)
			if err != nil {
				unsubscribe()
				return nil, err
			}
			console.write(pluginLine("%s loaded", greeterPluginID))
			return func() {
				unsubscribe()
				unregister()
				console.write(pluginLine("%s unloaded", greeterPluginID))
			}, nil
		},
	}
}

// permissionPlugin adds an "ask" rule for bash to the build agent, which
// otherwise allows every tool, so interactive approval has something to ask
// about. Registered through ClientOptions.Plugins when not running with --yolo.
func permissionPlugin() agent.PluginConfig {
	return agent.PluginConfig{
		ID: permissionPluginID,
		Func: func(pluginCtx *agent.PluginContext) (func(), error) {
			_, err := pluginCtx.Agents.Transform(func(draft core.AgentDraft) error {
				draft.Update(schema.AgentID(agentName), func(info *schema.AgentInfo) {
					info.Permissions = append(info.Permissions,
						schema.PermissionRule{Action: "bash", Resource: "*", Effect: schema.PermissionEffectAsk},
						schema.PermissionRule{Action: convertLengthName, Resource: "*", Effect: schema.PermissionEffectAsk},
					)
				})
				return nil
			})
			return func() {}, err
		},
	}
}

type pluginGreetInput struct {
	Name string `json:"name" jsonschema:"Name to greet"`
}

// pluginCommand unloads a loaded plugin at runtime: "/plugin remove <id>".
// Removing runs the plugin's cleanup (unsubscribe, unregister).
func (c *chat) pluginCommand(input string) {
	args := commandFields(input, "/plugin")
	if len(args) != 2 || args[0] != "remove" {
		c.console.write("usage: /plugin remove <id>  (configured: " + greeterPluginID + ")\n")
		return
	}
	id := args[1]
	if !c.plugins.has(id) {
		c.console.write(fmt.Sprintf("plugin %s is not active\n", id))
		return
	}
	if err := c.client.Plugin().Remove(schema.PluginID(id)); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.plugins.remove(id)
	c.console.write(fmt.Sprintf("removed plugin %s%s%s\n", cyan, id, reset))
}
