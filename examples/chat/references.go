package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/schema"
)

// referenceCommand handles "/reference" (list), "/reference add <name> <path>",
// "/reference add-git <name> <repo> [<branch>]" and "/reference remove <name>".
// References are named directories injected into the model's system context. A
// git source schedules a shallow clone into the cache in the background; the
// reference is advertised, with the checkout's eventual path, from the next
// turn.
func (c *chat) referenceCommand(input string) {
	args := commandFields(input, "/reference")
	references := c.client.Reference()
	switch {
	case len(args) == 0:
		c.listReferences(references)
	case args[0] == "add" && len(args) == 3:
		c.addReference(references, args[1], args[2])
	case args[0] == "add-git" && (len(args) == 3 || len(args) == 4):
		c.addGitReference(references, args[1], args[2], args[3:])
	case args[0] == "remove" && len(args) == 2:
		c.removeReference(references, args[1])
	default:
		c.console.write("usage: /reference [add <name> <path> | add-git <name> <repo> [<branch>] | remove <name>]\n")
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
		c.console.write(fmt.Sprintf("  %s%s%s %s— %s%s\n", cyan, info.Name, reset, dim, describe(info.Description), reset))
		c.console.write(fmt.Sprintf("    %s%s%s %s%s%s\n", dim, info.Path, reset, dim, referenceState(info), reset))
	}
}

// referenceState labels a reference's source and, for git, whether its checkout
// exists yet — the clone runs in the background.
func referenceState(info schema.ReferenceInfo) string {
	if info.Source.ReferenceSourceType() != "git" {
		return "[local]"
	}
	if _, err := os.Stat(string(info.Path)); err == nil {
		return "[git] ready"
	}
	return "[git] cloning..."
}

func (c *chat) addReference(references *core.Reference, name, path string) {
	description := "Added via /reference"
	if !c.transformReferences(references, func(draft core.ReferenceDraft) {
		draft.Add(name, core.ReferenceLocalSource{
			Type:        "local",
			Path:        schema.AbsolutePath(expandPath(path)),
			Description: &description,
		})
	}) {
		return
	}
	c.console.write(fmt.Sprintf("added reference %s%s%s\n", cyan, name, reset))
}

// addGitReference adds a git reference and reports whether it materialized: the
// SDK silently skips a non-remote repository or an invalid branch, so the result
// is confirmed against the materialized list.
func (c *chat) addGitReference(references *core.Reference, name, repository string, branchArgs []string) {
	source := core.ReferenceGitSource{Type: "git", Repository: repository}
	description := "Cloned from " + repository
	if len(branchArgs) == 1 {
		branch := branchArgs[0]
		source.Branch = &branch
		description += "@" + branch
	}
	source.Description = &description
	if !c.transformReferences(references, func(draft core.ReferenceDraft) {
		draft.Add(name, source)
	}) {
		return
	}
	info, ok := findReference(references, name)
	if !ok {
		c.console.write(fmt.Sprintf("%s%s was not accepted (needs a remote repository and a valid branch)%s\n", yellow, name, reset))
		return
	}
	c.console.write(fmt.Sprintf("added git reference %s%s%s → %s%s%s (cloning in the background; /reference shows progress)\n",
		cyan, name, reset, dim, info.Path, reset))
}

func (c *chat) removeReference(references *core.Reference, name string) {
	if !c.transformReferences(references, func(draft core.ReferenceDraft) {
		draft.Remove(name)
	}) {
		return
	}
	c.console.write(fmt.Sprintf("removed reference %s%s%s\n", cyan, name, reset))
}

// transformReferences applies a draft edit, reporting any error to the console.
func (c *chat) transformReferences(references *core.Reference, edit func(core.ReferenceDraft)) bool {
	if _, err := references.Transform(func(draft core.ReferenceDraft) error {
		edit(draft)
		return nil
	}); err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return false
	}
	return true
}

// findReference returns the materialized reference with the given name.
func findReference(references *core.Reference, name string) (schema.ReferenceInfo, bool) {
	for _, info := range references.List() {
		if info.Name == name {
			return info, true
		}
	}
	return schema.ReferenceInfo{}, false
}
