package config

import (
	"encoding/json"

	v2 "github.com/user/go-agent-sdk/core/config"
	"github.com/user/go-agent-sdk/schema"
)

// V1WatcherInfo mirrors the inline watcher struct in ConfigV1.Info.
type V1WatcherInfo struct {
	Ignore []string `json:"ignore,omitempty"`
}

// V1Enterprise mirrors the inline enterprise struct in ConfigV1.Info.
type V1Enterprise struct {
	URL *string `json:"url,omitempty"`
}

// V1Experimental mirrors the inline experimental struct in ConfigV1.Info.
type V1Experimental struct {
	DisablePasteSummary *bool                   `json:"disable_paste_summary,omitempty"`
	BatchTool           *bool                   `json:"batch_tool,omitempty"`
	OpenTelemetry       *bool                   `json:"openTelemetry,omitempty"`
	PrimaryTools        []string                `json:"primary_tools,omitempty"`
	ContinueLoopOnDeny  *bool                   `json:"continue_loop_on_deny,omitempty"`
	MCPTimeout          *schema.PositiveInt     `json:"mcp_timeout,omitempty"`
	Policies            []v2.ExperimentalPolicy `json:"policies,omitempty"`
}

// V1Compaction mirrors the inline compaction struct in ConfigV1.Info.
type V1Compaction struct {
	Auto                 *bool                  `json:"auto,omitempty"`
	Prune                *bool                  `json:"prune,omitempty"`
	TailTurns            *schema.NonNegativeInt `json:"tail_turns,omitempty"`
	PreserveRecentTokens *schema.NonNegativeInt `json:"preserve_recent_tokens,omitempty"`
	Reserved             *schema.NonNegativeInt `json:"reserved,omitempty"`
}

// V1Info mirrors ConfigV1.Info.
type V1Info struct {
	Schema            *string                   `json:"$schema,omitempty"`
	Shell             *string                   `json:"shell,omitempty"`
	LogLevel          *string                   `json:"logLevel,omitempty"`
	Server            *V1ServerInfo             `json:"server,omitempty"`
	Command           map[string]V1CommandInfo  `json:"command,omitempty"`
	Skills            *V1SkillsInfo             `json:"skills,omitempty"`
	References        v2.ReferenceInfo          `json:"references,omitempty"`
	Reference         v2.ReferenceInfo          `json:"reference,omitempty"`
	Watcher           *V1WatcherInfo            `json:"watcher,omitempty"`
	Snapshot          *bool                     `json:"snapshot,omitempty"`
	Plugin            []V1PluginSpec            `json:"plugin,omitempty"`
	Share             *string                   `json:"share,omitempty"`
	Autoshare         *bool                     `json:"autoshare,omitempty"`
	Autoupdate        json.RawMessage           `json:"autoupdate,omitempty"`
	DisabledProviders []string                  `json:"disabled_providers,omitempty"`
	EnabledProviders  []string                  `json:"enabled_providers,omitempty"`
	Model             *string                   `json:"model,omitempty"`
	SmallModel        *string                   `json:"small_model,omitempty"`
	DefaultAgent      *string                   `json:"default_agent,omitempty"`
	SubagentDepth     *schema.NonNegativeInt    `json:"subagent_depth,omitempty"`
	Username          *string                   `json:"username,omitempty"`
	Mode              map[string]V1AgentInfo    `json:"mode,omitempty"`
	Agent             map[string]V1AgentInfo    `json:"agent,omitempty"`
	Provider          map[string]V1ProviderInfo `json:"provider,omitempty"`
	MCP               map[string]V1MCPServer    `json:"mcp,omitempty"`
	Formatter         V1FormatterInfo           `json:"formatter,omitempty"`
	LSP               V1LSPInfo                 `json:"lsp,omitempty"`
	Instructions      []string                  `json:"instructions,omitempty"`
	Layout            *string                   `json:"layout,omitempty"`
	Permission        V1PermissionInfo          `json:"permission,omitempty"`
	Tools             json.RawMessage           `json:"tools,omitempty"`
	Attachment        *V1AttachmentInfo         `json:"attachment,omitempty"`
	Enterprise        *V1Enterprise             `json:"enterprise,omitempty"`
	ToolOutput        *v2.ToolOutputInfo        `json:"tool_output,omitempty"`
	Compaction        *V1Compaction             `json:"compaction,omitempty"`
	Experimental      *V1Experimental           `json:"experimental,omitempty"`
}
