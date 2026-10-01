package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/schema"
)

// referenceCommand handles "/reference" (list), "/reference add <name> <path>"
// and "/reference remove <name>". References are named directories injected into
// the model's system context; a local source added here is materialized and
// advertised on the next turn.
func (c *chat) referenceCommand(input string) {
	args := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "/reference")))
	references := c.client.Reference()
	switch {
	case len(args) == 0:
		c.listReferences(references)
	case args[0] == "add" && len(args) == 3:
		c.addReference(references, args[1], args[2])
	case args[0] == "remove" && len(args) == 2:
		c.removeReference(references, args[1])
	default:
		c.console.write("usage: /reference [add <name> <path> | remove <name>]\n")
	}
}

// listReferences prints the configured references, sorted by name.
func (c *chat) listReferences(references *core.Reference) {
	infos := references.List()
	if len(infos) == 0 {
		c.console.write("(no references; /reference add <name> <path>)\n")
		return
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	c.console.write(fmt.Sprintf("%sReferences (%d):%s\n", bold, len(infos), reset))
	for _, info := range infos {
		c.console.write(fmt.Sprintf("  %s%s%s — %s\n", cyan, info.Name, reset, describe(info.Description)))
		c.console.write(fmt.Sprintf("    %s%s%s\n", dim, info.Path, reset))
	}
}

func (c *chat) addReference(references *core.Reference, name, path string) {
	description := "Added via /reference"
	if _, err := references.Transform(func(draft core.ReferenceDraft) error {
		draft.Add(name, core.ReferenceLocalSource{
			Type:        "local",
			Path:        schema.AbsolutePath(expandPath(path)),
			Description: &description,
		})
		return nil
	}); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("added reference %s%s%s\n", cyan, name, reset))
}

func (c *chat) removeReference(references *core.Reference, name string) {
	if _, err := references.Transform(func(draft core.ReferenceDraft) error {
		draft.Remove(name)
		return nil
	}); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.console.write(fmt.Sprintf("removed reference %s%s%s\n", cyan, name, reset))
}
