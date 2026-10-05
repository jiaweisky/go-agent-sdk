package main

import (
	"fmt"
	"strings"

	"github.com/jiaweisky/go-agent-sdk/core"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// diffFile is one file's diff, from a snapshot diff or a tool's structured output.
type diffFile struct {
	Path      string
	Additions int
	Deletions int
	Patch     string
}

// writeStepFiles prints a one-line summary of the files a step changed.
func (c *chat) writeStepFiles(files []schema.RelativePath) {
	if len(files) == 0 {
		return
	}
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = string(file)
	}
	c.console.write(fmt.Sprintf("%s[files]%s %d changed: %s\n", dim, reset, len(files), summarizeNames(names)))
}

// summarizeNames joins up to three names, then "+N more".
func summarizeNames(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(names[:3], ", "), len(names)-3)
}

// writeToolDiff prints the diffs an edit/apply_patch tool returned. The write
// tool has no patch in its output, so a created file shows only in /diff.
func (c *chat) writeToolDiff(structured any) {
	if files := toolDiffFiles(structured); len(files) > 0 {
		c.writeDiffs(files)
	}
}

// writeDiffs prints each file's patch with a header and +/- coloring.
func (c *chat) writeDiffs(files []diffFile) {
	for _, file := range files {
		c.console.write(fmt.Sprintf("%s[diff]%s %s %s(+%d -%d)%s\n",
			dim, reset, file.Path, dim, file.Additions, file.Deletions, reset))
		c.writePatch(file.Patch)
	}
}

// writePatch prints a unified diff patch, coloring added and removed lines.
func (c *chat) writePatch(patch string) {
	if strings.TrimSpace(patch) == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(patch, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "@@"):
			c.console.write(cyan + line + reset + "\n")
		case strings.HasPrefix(line, "+"):
			c.console.write(green + line + reset + "\n")
		case strings.HasPrefix(line, "-"):
			c.console.write(red + line + reset + "\n")
		default:
			c.console.write(line + "\n")
		}
	}
}

// diffCommand prints the diff of everything the turn changed, from the turn's
// start and latest snapshots.
func (c *chat) diffCommand() {
	c.mu.Lock()
	from, to := c.turnStartSnapshot, c.lastSnapshot
	c.mu.Unlock()
	if from == "" || to == "" {
		c.console.write("(no snapshot for this turn; snapshots need a git project)\n")
		return
	}
	diffs, err := c.client.Diff(from, to)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	if len(diffs) == 0 {
		c.console.write("(no changes this turn)\n")
		return
	}
	c.writeDiffs(snapshotDiffFiles(diffs))
}

// captureTurnStart records the snapshot at the turn's start for /diff.
func (c *chat) captureTurnStart() { c.recordSnapshot(&c.turnStartSnapshot) }

// captureTurnEnd records the snapshot after the turn, so /diff covers the whole
// turn even when it ended without a final step.ended (a failure or interrupt).
func (c *chat) captureTurnEnd() { c.recordSnapshot(&c.lastSnapshot) }

// recordSnapshot captures the current snapshot into target, if supported.
func (c *chat) recordSnapshot(target *string) {
	if id, ok := c.client.Capture(); ok {
		c.mu.Lock()
		*target = id
		c.mu.Unlock()
	}
}

// toolDiffFiles extracts per-file diffs from a tool's structured output.
func toolDiffFiles(structured any) []diffFile {
	object, ok := structured.(map[string]any)
	if !ok {
		return nil
	}
	entries, ok := object["files"].([]any)
	if !ok {
		return nil
	}
	var files []diffFile
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		patch, _ := item["patch"].(string)
		if patch == "" {
			continue
		}
		files = append(files, diffFile{
			Path:      stringField(item, "file"),
			Additions: intField(item, "additions"),
			Deletions: intField(item, "deletions"),
			Patch:     patch,
		})
	}
	return files
}

// snapshotDiffFiles converts core snapshot diffs into the shared diffFile shape.
func snapshotDiffFiles(diffs []core.FileDiff) []diffFile {
	files := make([]diffFile, 0, len(diffs))
	for _, diff := range diffs {
		files = append(files, diffFile{
			Path:      string(diff.Path),
			Additions: int(diff.Additions),
			Deletions: int(diff.Deletions),
			Patch:     diff.Patch,
		})
	}
	return files
}

func intField(object map[string]any, key string) int {
	if value, ok := object[key].(float64); ok {
		return int(value)
	}
	return 0
}
