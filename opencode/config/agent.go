package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk/core"
	coreconfig "github.com/user/go-agent-sdk/core/config"
	"github.com/user/go-agent-sdk/core/util"
	v1config "github.com/user/go-agent-sdk/core/v1/config"
)

// AgentEntry is an agent definition loaded from a markdown file, preserving the
// scan order so later definitions win when merged.
type AgentEntry struct {
	Name string
	Info v1config.V1AgentInfo
	// Data is the raw frontmatter, used to pick the v2 vs legacy decode path.
	Data map[string]any
	// Content is the trimmed markdown body.
	Content string
	// Primary marks entries discovered under {mode,modes}.
	Primary bool
}

// LoadAgents mirrors ConfigAgent.load: it loads agent definitions from
// `{agent,agents}/**/*.md` under dir.
func LoadAgents(dir string) (map[string]v1config.V1AgentInfo, error) {
	return agentMap(loadAgentFiles(dir, "{agent,agents}/**/*.md", []string{"agent/", "agents/"}, false))
}

// LoadModes mirrors ConfigAgent.loadMode: it loads primary agents from
// `{mode,modes}/*.md` under dir.
func LoadModes(dir string) (map[string]v1config.V1AgentInfo, error) {
	return agentMap(loadAgentFiles(dir, "{mode,modes}/*.md", []string{"mode/", "modes/"}, true))
}

// LoadAgentEntries returns the agent files followed by the mode files, each in
// sorted scan order, mirroring ConfigAgentPlugin's legacySources discovery.
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
	for _, item := range matches {
		content, err := os.ReadFile(item)
		if err != nil {
			continue
		}
		markdown, ok := coreconfig.ParseMarkdownOption(string(content))
		if !ok {
			continue
		}
		relative, err := filepath.Rel(dir, item)
		if err != nil {
			continue
		}
		name := ConfigEntryNameFromPath(relative, prefixes)
		info, ok := decodeAgentInfo(markdown)
		if !ok {
			continue
		}
		if primary {
			mode := "primary"
			info.Mode = &mode
		}
		data, _ := markdown.Data.(map[string]any)
		result = append(result, AgentEntry{
			Name: name, Info: info, Data: data,
			Content: strings.TrimSpace(markdown.Content), Primary: primary,
		})
	}
	return result, nil
}

func agentMap(entries []AgentEntry, err error) (map[string]v1config.V1AgentInfo, error) {
	if err != nil {
		return nil, err
	}
	result := map[string]v1config.V1AgentInfo{}
	for _, entry := range entries {
		result[entry.Name] = entry.Info
	}
	return result, nil
}

func decodeAgentInfo(markdown coreconfig.MarkdownResult) (v1config.V1AgentInfo, bool) {
	encoded, err := json.Marshal(markdown.Data)
	if err != nil {
		return v1config.V1AgentInfo{}, false
	}
	var info v1config.V1AgentInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		return v1config.V1AgentInfo{}, false
	}
	prompt := strings.TrimSpace(markdown.Content)
	info.Prompt = &prompt
	return info, true
}
