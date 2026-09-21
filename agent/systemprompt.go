package agent

import (
	"strings"

	"github.com/user/go-agent-sdk/schema"
)

// System prompt modes.
const (
	SystemPromptAppend    = "append"
	SystemPromptReplace   = "replace"
	SystemPromptCustomize = "customize"
)

// System prompt section identifiers. Sections are a coarser model than
// copilot-sdk: this SDK composes the system prompt from the agent's system text
// plus the location context, so the addressable sections are the agent system
// text and the additional instructions block.
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
	// Sections selectively overrides named sections in customize mode.
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
		sections := map[string]string{
			SectionAgentSystem:  base,
			SectionInstructions: cfg.Content,
		}
		for name, override := range cfg.Sections {
			current, ok := sections[name]
			if !ok {
				continue
			}
			sections[name] = applySection(current, override)
		}
		combined := joinSections(sections[SectionAgentSystem], sections[SectionInstructions])
		info.System = &combined
	default: // append
		combined := joinSections(base, cfg.Content)
		info.System = &combined
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
