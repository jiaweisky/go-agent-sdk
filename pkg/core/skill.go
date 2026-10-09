package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jiaweisky/go-agent-sdk/internal/core/util"
	skillsub "github.com/jiaweisky/go-agent-sdk/pkg/core/skill"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type (
	SkillDirectorySource = schema.SkillDirectorySource
	SkillURLSource       = schema.SkillURLSource
	SkillEmbeddedSource  = schema.SkillEmbeddedSource
	SkillSource          = schema.SkillSource
	SkillInfo            = schema.SkillInfo
)

// SkillAvailable filters the skills to those the agent may use.
func SkillAvailable(skills []schema.SkillInfo, agent schema.AgentInfo) []schema.SkillInfo {
	var result []schema.SkillInfo
	for _, skill := range skills {
		if PermissionEvaluate("skill", skill.Name, agent.Permissions).Effect != PermissionEffectDeny {
			result = append(result, skill)
		}
	}
	return result
}

type skillData struct {
	sources []schema.SkillSource
}

// SkillDraft is the mutable view passed to registered transforms.
type SkillDraft struct {
	Source func(schema.SkillSource)
	List   func() []schema.SkillSource
}

// SkillDiscovery is the narrow discovery surface Skill depends on.
type SkillDiscovery interface {
	Pull(url string) ([]schema.AbsolutePath, error)
}

// Skill is the skill registry.
type Skill struct {
	state     *State[*skillData, SkillDraft]
	discovery SkillDiscovery
	cacheMu   sync.Mutex
	cache     map[string][]schema.SkillInfo
}

func NewSkill(discovery SkillDiscovery, batch ...*StateBatch) *Skill {
	skill := &Skill{discovery: discovery, cache: map[string][]schema.SkillInfo{}}
	options := StateOptions[*skillData, SkillDraft]{
		Initial: func() *skillData { return &skillData{} },
		Draft: func(data *skillData) SkillDraft {
			return SkillDraft{
				Source: func(source schema.SkillSource) {
					for _, item := range data.sources {
						if schema.SkillSourceEquals(item, source) {
							return
						}
					}
					data.sources = append(data.sources, source)
				},
				List: func() []schema.SkillSource {
					return data.sources
				},
			}
		},
	}
	if len(batch) > 0 {
		options.Batch = batch[0]
	}
	skill.state = NewState(options)
	return skill
}

func (s *Skill) Transform(update func(SkillDraft) error) (func() error, error) {
	return s.state.Transform(update)
}

func (s *Skill) Reload() error {
	return s.state.Reload()
}

func (s *Skill) Sources() []schema.SkillSource {
	return s.state.Get().sources
}

// List resolves and caches every configured skill source.
func (s *Skill) List() ([]schema.SkillInfo, error) {
	skills := map[string]schema.SkillInfo{}
	var order []string
	for _, source := range s.state.Get().sources {
		key := schema.SkillSourceKey(source)
		s.cacheMu.Lock()
		loaded, ok := s.cache[key]
		s.cacheMu.Unlock()
		if !ok {
			var err error
			loaded, err = s.load(source)
			if err != nil {
				return nil, err
			}
			s.cacheMu.Lock()
			s.cache[key] = loaded
			s.cacheMu.Unlock()
		}
		for _, skill := range loaded {
			if _, exists := skills[skill.Name]; !exists {
				order = append(order, skill.Name)
			}
			skills[skill.Name] = skill
		}
	}
	result := make([]schema.SkillInfo, 0, len(order))
	for _, name := range order {
		result = append(result, skills[name])
	}
	return result, nil
}

func (s *Skill) load(source schema.SkillSource) ([]schema.SkillInfo, error) {
	if embedded, ok := source.(schema.SkillEmbeddedSource); ok {
		return []schema.SkillInfo{embedded.Skill}, nil
	}

	var directories []string
	switch src := source.(type) {
	case schema.SkillDirectorySource:
		directories = append(directories, string(src.Path))
	case schema.SkillURLSource:
		pulled, err := s.discovery.Pull(src.URL)
		if err != nil {
			return nil, err
		}
		for _, directory := range pulled {
			directories = append(directories, string(directory))
		}
	}

	var skills []schema.SkillInfo
	for _, directory := range directories {
		files, err := util.GlobScan("{*.md,**/SKILL.md}", util.GlobOptions{
			Cwd: directory, Absolute: true, Include: "file", Symlink: true, Dot: true,
		})
		if err != nil {
			files = nil
		}
		sort.Strings(files)
		for _, file := range files {
			content, ok := ReadFileStringSafe(file)
			if !ok || content == "" {
				continue
			}
			markdown, ok := util.ParseMarkdownOption(content)
			if !ok {
				continue
			}
			frontmatter, ok := decodeSkillFrontmatter(markdown.Data)
			if !ok {
				continue
			}
			var name string
			if frontmatter.name != nil {
				name = *frontmatter.name
			} else if filepath.Dir(file) == directory {
				name = strings.TrimSuffix(filepath.Base(file), ".md")
			}
			if name == "" {
				continue
			}
			skills = append(skills, schema.SkillInfo{
				Name:        name,
				Description: frontmatter.description,
				Slash:       frontmatter.slash,
				Location:    schema.AbsolutePath(file),
				Content:     markdown.Content,
			})
		}
	}
	return skills, nil
}

type skillFrontmatter struct {
	name        *string
	description *string
	slash       *bool
}

func decodeSkillFrontmatter(value any) (skillFrontmatter, bool) {
	data, ok := value.(map[string]any)
	if !ok {
		return skillFrontmatter{}, false
	}
	var result skillFrontmatter
	if value, ok := data["name"]; ok {
		text, ok := value.(string)
		if !ok {
			return skillFrontmatter{}, false
		}
		result.name = &text
	}
	if value, ok := data["description"]; ok {
		text, ok := value.(string)
		if !ok {
			return skillFrontmatter{}, false
		}
		result.description = &text
	}
	if value, ok := data["slash"]; ok {
		flag, ok := value.(bool)
		if !ok {
			return skillFrontmatter{}, false
		}
		result.slash = &flag
	}
	return result, true
}

// NewSkillDiscovery wires the core filesystem helpers into SkillDiscovery.
func NewSkillDiscovery(cacheDir string, client skillsub.HTTPClient) *skillsub.Discovery {
	return skillsub.NewDiscovery(skillFileSystem{}, cacheDir, client)
}

type skillFileSystem struct{}

func (skillFileSystem) Exists(path string) (bool, error) { return Exists(path) }

func (skillFileSystem) ReadFileStringSafe(path string) (string, bool) {
	return ReadFileStringSafe(path)
}

func (skillFileSystem) WriteWithDirs(path string, content []byte, mode ...os.FileMode) error {
	return WriteWithDirs(path, content, mode...)
}

func (skillFileSystem) WriteFileString(path string, content string, mode ...os.FileMode) error {
	return WriteFileString(path, content, mode...)
}

func (skillFileSystem) Rename(oldPath, newPath string) error { return Rename(oldPath, newPath) }

func (skillFileSystem) RemoveAll(path string) error { return RemoveAll(path) }

func (skillFileSystem) Contains(parent, child string) bool { return Contains(parent, child) }
