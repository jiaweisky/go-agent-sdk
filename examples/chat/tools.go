package main

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/user/go-agent-sdk"
)

// runtimeTools are the tools /tool can register at runtime, keyed by name.
var runtimeTools = map[string]agent.Tool{
	"now": agent.DefineTool("now", "Return the current date and time in RFC3339.",
		func(_ struct{}, _ agent.ToolContext) (string, error) {
			return time.Now().Format(time.RFC3339), nil
		}),
	"dice": agent.DefineTool("dice", "Roll a six-sided die and return 1-6.",
		func(_ struct{}, _ agent.ToolContext) (int, error) {
			return rand.IntN(6) + 1, nil
		}),
	convertLengthName:   convertLengthTool(),
	"list_length_units": listUnitsTool(),
}

// runtimeTool returns one of the tools /tool can register, and reports whether
// the name is known.
func runtimeTool(name string) (agent.Tool, bool) {
	tool, ok := runtimeTools[name]
	return tool, ok
}

// runtimeToolNames lists the runtime tool names, sorted.
func runtimeToolNames() []string {
	names := make([]string, 0, len(runtimeTools))
	for name := range runtimeTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// toolsCommand handles "/tool" (list), "/tool add <name>" and
// "/tool remove <name>". It uses Client.RegisterTools: tools added here become
// available to the live session on the next turn, without restarting.
func (c *chat) toolsCommand(input string) {
	args := commandFields(input, "/tool")
	if len(args) == 0 {
		c.listRuntimeTools()
		return
	}
	if len(args) != 2 {
		c.console.write("usage: /tool [add|remove <name>]\n")
		return
	}
	switch args[0] {
	case "add":
		c.addRuntimeTool(args[1])
	case "remove":
		c.removeRuntimeTool(args[1])
	default:
		c.console.write("usage: /tool [add|remove <name>]\n")
	}
}

// listRuntimeTools prints the tools added with /tool.
func (c *chat) listRuntimeTools() {
	c.mu.Lock()
	names := make([]string, 0, len(c.tools))
	for name := range c.tools {
		names = append(names, name)
	}
	c.mu.Unlock()
	if len(names) == 0 {
		c.console.write(fmt.Sprintf("(no runtime tools; /tool add %s)\n", strings.Join(runtimeToolNames(), " | ")))
		return
	}
	sort.Strings(names)
	c.console.write(fmt.Sprintf("%sRuntime tools (%d):%s\n", bold, len(names), reset))
	for _, name := range names {
		c.console.write(fmt.Sprintf("  %s%s%s\n", cyan, name, reset))
	}
}

func (c *chat) addRuntimeTool(name string) {
	tool, ok := runtimeTool(name)
	if !ok {
		c.console.write(fmt.Sprintf("unknown tool: %s (try: %s)\n", name, strings.Join(runtimeToolNames(), ", ")))
		return
	}
	c.mu.Lock()
	_, exists := c.tools[name]
	c.mu.Unlock()
	if exists {
		c.console.write(fmt.Sprintf("%s is already registered\n", name))
		return
	}
	unregister, err := c.client.RegisterTools(tool)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.mu.Lock()
	c.tools[name] = unregister
	c.mu.Unlock()
	c.console.write(fmt.Sprintf("registered tool %s%s%s\n", cyan, name, reset))
}

func (c *chat) removeRuntimeTool(name string) {
	c.mu.Lock()
	unregister := c.tools[name]
	delete(c.tools, name)
	c.mu.Unlock()
	if unregister == nil {
		c.console.write(fmt.Sprintf("%s is not registered\n", name))
		return
	}
	unregister()
	c.console.write(fmt.Sprintf("removed tool %s%s%s\n", cyan, name, reset))
}
