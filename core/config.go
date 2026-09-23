package core

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/user/go-agent-sdk/core/config"
	"github.com/user/go-agent-sdk/core/util"
	v1config "github.com/user/go-agent-sdk/core/v1/config"
	"github.com/user/go-agent-sdk/schema"
)

// ConfigAutoupdate mirrors the `boolean | "notify"` union.
type ConfigAutoupdate struct {
	Bool   *bool
	Notify bool
}

func (a *ConfigAutoupdate) UnmarshalJSON(data []byte) error {
	if string(data) == `"notify"` {
		a.Notify = true
		a.Bool = nil
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	a.Bool = &value
	a.Notify = false
	return nil
}

func (a ConfigAutoupdate) MarshalJSON() ([]byte, error) {
	if a.Notify {
		return []byte(`"notify"`), nil
	}
	if a.Bool != nil {
		return json.Marshal(*a.Bool)
	}
	return []byte("null"), nil
}

// ConfigEnterprise mirrors the inline enterprise struct.
type ConfigEnterprise struct {
	URL *string `json:"url,omitempty"`
}

// ConfigInfo mirrors Config.Info.
type ConfigInfo struct {
	Schema        *string                       `json:"$schema,omitempty"`
	Shell         *string                       `json:"shell,omitempty"`
	Model         *string                       `json:"model,omitempty"`
	DefaultAgent  *string                       `json:"default_agent,omitempty"`
	SubagentDepth *schema.NonNegativeInt        `json:"subagent_depth,omitempty"`
	Autoupdate    *ConfigAutoupdate             `json:"autoupdate,omitempty"`
	Share         *string                       `json:"share,omitempty"`
	Enterprise    *ConfigEnterprise             `json:"enterprise,omitempty"`
	Username      *string                       `json:"username,omitempty"`
	Permissions   schema.PermissionRuleset      `json:"permissions,omitempty"`
	Agents        map[string]config.AgentInfo   `json:"agents,omitempty"`
	Snapshots     *bool                         `json:"snapshots,omitempty"`
	Watcher       *config.WatcherInfo           `json:"watcher,omitempty"`
	Formatter     config.FormatterInfo          `json:"formatter,omitempty"`
	LSP           config.LSPInfo                `json:"lsp,omitempty"`
	Attachments   *config.AttachmentsInfo       `json:"attachments,omitempty"`
	ToolOutput    *config.ToolOutputInfo        `json:"tool_output,omitempty"`
	MCP           *config.MCPInfo               `json:"mcp,omitempty"`
	Compaction    *config.CompactionInfo        `json:"compaction,omitempty"`
	Skills        []string                      `json:"skills,omitempty"`
	Commands      map[string]config.CommandInfo `json:"commands,omitempty"`
	Instructions  []string                      `json:"instructions,omitempty"`
	References    config.ReferenceInfo          `json:"references,omitempty"`
	Plugins       config.Plugins                `json:"plugins,omitempty"`
	Experimental  *config.ExperimentalInfo      `json:"experimental,omitempty"`
	Providers     map[string]config.Info        `json:"providers,omitempty"`
}

// ConfigDocument mirrors Config.Document.
type ConfigDocument struct {
	Path *string
	Info ConfigInfo
}

func (ConfigDocument) configEntryTag() {}

// ConfigDirectory mirrors Config.Directory.
type ConfigDirectory struct {
	Path schema.AbsolutePath
}

func (ConfigDirectory) configEntryTag() {}

// ConfigEntry is the Config.Entry union.
type ConfigEntry interface {
	configEntryTag()
}

var configFileNames = []string{"opencode.json", "opencode.jsonc"}

// Latest mirrors Config.latest: the value from the highest-priority document
// that defines it.
func Latest[K any](entries []ConfigEntry, get func(ConfigInfo) *K) *K {
	for i := len(entries) - 1; i >= 0; i-- {
		document, ok := entries[i].(ConfigDocument)
		if !ok {
			continue
		}
		if value := get(document.Info); value != nil {
			return value
		}
	}
	return nil
}

// Config mirrors core/config.ts's service interface.
type Config interface {
	Entries() []ConfigEntry
}

type configService struct {
	entries []ConfigEntry
}

func (c *configService) Entries() []ConfigEntry {
	return c.entries
}

// ConfigDeps are the constructor-injected dependencies of the config service.
type ConfigDeps struct {
	Global   Global
	Location *Location
	Policy   Policy
}

// NewConfig mirrors the Config.layer effect: it discovers and loads location
// config documents once, then returns them from Entries.
func NewConfig(deps ConfigDeps) (Config, error) {
	globalDirectory := deps.Global.Config
	locationDirectory := string(deps.Location.Directory)
	projectDirectory := string(deps.Location.Project.Directory)
	locationIsGlobal := configResolve(locationDirectory) == configResolve(globalDirectory)

	var discovered []string
	if !locationIsGlobal {
		discovered = Up(
			[]string{".opencode", "opencode.jsonc", "opencode.json"},
			locationDirectory,
			projectDirectory,
		)
	}

	directories := []string{globalDirectory}
	for i := len(discovered) - 1; i >= 0; i-- {
		if filepath.Base(discovered[i]) == ".opencode" {
			directories = append(directories, discovered[i])
		}
	}

	var direct []ConfigEntry
	for i := len(discovered) - 1; i >= 0; i-- {
		if filepath.Base(discovered[i]) == ".opencode" {
			continue
		}
		if document := loadConfigFile(discovered[i]); document != nil {
			direct = append(direct, *document)
		}
	}

	supplementary := make([][]ConfigEntry, 0, len(directories))
	for _, directory := range directories {
		supplementary = append(supplementary, loadConfigDirectory(directory))
	}

	var configs []ConfigEntry
	if len(supplementary) > 0 {
		configs = append(configs, supplementary[0]...)
	}
	configs = append(configs, direct...)
	for _, extra := range supplementary[1:] {
		configs = append(configs, extra...)
	}

	var statements []schema.PolicyInfo
	for i := len(configs) - 1; i >= 0; i-- {
		document, ok := configs[i].(ConfigDocument)
		if !ok || document.Info.Experimental == nil {
			continue
		}
		statements = append(statements, document.Info.Experimental.Policies...)
	}
	deps.Policy.Load(statements)

	return &configService{entries: configs}, nil
}

func configResolve(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return absolute
}

func loadConfigDirectory(directory string) []ConfigEntry {
	entries := make([]ConfigEntry, 0, len(configFileNames)+1)
	for _, name := range configFileNames {
		if document := loadConfigFile(filepath.Join(directory, name)); document != nil {
			entries = append(entries, *document)
		}
	}
	entries = append(entries, ConfigDirectory{Path: schema.AbsolutePath(directory)})
	return entries
}

func loadConfigFile(file string) *ConfigDocument {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	clean, err := util.CleanJSONC(string(content))
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(clean, &raw); err != nil {
		return nil
	}

	var info ConfigInfo
	if v1config.V1IsV1(raw) {
		var legacy v1config.V1Info
		if err := json.Unmarshal(clean, &legacy); err != nil {
			return nil
		}
		encoded, err := json.Marshal(v1config.V1Migrate(legacy))
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(encoded, &info); err != nil {
			return nil
		}
	} else if err := json.Unmarshal(clean, &info); err != nil {
		return nil
	}

	path := file
	return &ConfigDocument{Path: &path, Info: info}
}
