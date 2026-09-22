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

// LoadAgents mirrors ConfigAgent.load: it loads agent definitions from
// `{agent,agents}/**/*.md` under dir.
func LoadAgents(dir string) (map[string]v1config.V1AgentInfo, error) {
	return loadAgentFiles(dir, "{agent,agents}/**/*.md", []string{"agent/", "agents/"}, false)
}

// LoadModes mirrors ConfigAgent.loadMode: it loads primary agents from
// `{mode,modes}/*.md` under dir.
func LoadModes(dir string) (map[string]v1config.V1AgentInfo, error) {
	return loadAgentFiles(dir, "{mode,modes}/*.md", []string{"mode/", "modes/"}, true)
}

func loadAgentFiles(dir, pattern string, prefixes []string, primary bool) (map[string]v1config.V1AgentInfo, error) {
	result := map[string]v1config.V1AgentInfo{}
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
		result[name] = info
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
