package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/util"
)

// AgentEntry is an agent markdown file (frontmatter + body) in scan order.
type AgentEntry struct {
	Name    string
	Data    map[string]any
	Content string
	// Primary marks entries discovered under {mode,modes}.
	Primary bool
}

// LoadAgentEntries returns {agent,agents}/**/*.md followed by {mode,modes}/*.md
// under dir, each in sorted scan order.
func LoadAgentEntries(dir string) ([]AgentEntry, error) {
	agents, err := loadAgentFiles(dir, "{agent,agents}/**/*.md", []string{"agent/", "agents/"}, false)
	if err != nil {
		return nil, err
	}
	modes, err := loadAgentFiles(dir, "{mode,modes}/*.md", []string{"mode/", "modes/"}, true)
	if err != nil {
		return nil, err
	}
	return append(agents, modes...), nil
}

func loadAgentFiles(dir, pattern string, prefixes []string, primary bool) ([]AgentEntry, error) {
	var result []AgentEntry
	matches, err := core.Glob(pattern, util.GlobOptions{
		Cwd: dir, Absolute: true, Dot: true, Symlink: true,
	})
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range matches {
		content, err := os.ReadFile(item)
		if err != nil {
			continue
		}
		markdown, ok := util.ParseMarkdownOption(string(content))
		if !ok {
			continue
		}
		relative, err := filepath.Rel(absDir, item)
		if err != nil {
			continue
		}
		data, _ := markdown.Data.(map[string]any)
		result = append(result, AgentEntry{
			Name:    ConfigEntryNameFromPath(relative, prefixes),
			Data:    data,
			Content: strings.TrimSpace(markdown.Content),
			Primary: primary,
		})
	}
	return result, nil
}
