package agent

import (
	"sort"
	"strings"

	"github.com/user/go-agent-sdk/schema"
)

// System prompt modes.
const (
	SystemPromptAppend    = "append"
	SystemPromptReplace   = "replace"
	SystemPromptCustomize = "customize"
)

// Built-in system prompt section identifiers.
//
// The SDK composes the system prompt from the agent's system text plus
// additional instructions; callers may also define arbitrary named sections via
// SystemPromptConfig.Sections. In customize mode the SDK renders sections in the
// order: agent_system, any custom sections (sorted by id), then instructions.
const (
	SectionAgentSystem  = "agent_system"
	SectionInstructions = "instructions"
)

// Section actions.
const (
	SectionReplace  = "replace"
	SectionRemove   = "remove"
	SectionAppend   = "append"
	SectionPrepend  = "prepend"
	SectionPreserve = "preserve"
)

// SystemPromptConfig customizes the system prompt for a session.
type SystemPromptConfig struct {
	// Mode is "append" (default), "replace", or "customize".
	Mode string
	// Content is appended (append/customize) or used as the whole prompt (replace).
	Content string
	// Sections selectively overrides named sections in customize mode. The
	// built-in ids are agent_system and instructions; any other id defines a
	// custom section whose override content is rendered between them.
	Sections map[string]SectionOverride
}

// SectionOverride describes a single section edit.
type SectionOverride struct {
	Action  string
	Content string
}

// applySystemPrompt mutates the agent system prompt according to the config.
func applySystemPrompt(info *schema.AgentInfo, cfg *SystemPromptConfig) {
	if info == nil || cfg == nil {
		return
	}
	base := ""
	if info.System != nil {
		base = *info.System
	}
	switch cfg.Mode {
	case SystemPromptReplace:
		system := cfg.Content
		info.System = &system
	case SystemPromptCustomize:
		values := map[string]string{
			SectionAgentSystem:  base,
			SectionInstructions: cfg.Content,
		}
		extras := make([]string, 0, len(cfg.Sections))
		for name, override := range cfg.Sections {
			values[name] = applySection(values[name], override)
			if name != SectionAgentSystem && name != SectionInstructions {
				extras = append(extras, name)
			}
		}
		sort.Strings(extras)
		order := append([]string{SectionAgentSystem}, extras...)
		order = append(order, SectionInstructions)
		parts := make([]string, 0, len(order))
		for _, name := range order {
			if values[name] != "" {
				parts = append(parts, values[name])
			}
		}
		system := strings.Join(parts, "\n\n")
		info.System = &system
	default: // append
		system := joinSections(base, cfg.Content)
		info.System = &system
	}
}

func applySection(current string, override SectionOverride) string {
	switch override.Action {
	case SectionReplace:
		return override.Content
	case SectionRemove:
		return ""
	case SectionPrepend:
		return joinSections(override.Content, current)
	case SectionAppend:
		return joinSections(current, override.Content)
	default:
		return current
	}
}

func joinSections(left, right string) string {
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return strings.TrimRight(left, "\n") + "\n\n" + right
	}
}
