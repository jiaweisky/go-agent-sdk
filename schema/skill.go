package schema

type SkillDirectorySource struct {
	Type string       `json:"type"`
	Path AbsolutePath `json:"path"`
}

func (SkillDirectorySource) SkillSourceType() string { return "directory" }

type SkillURLSource struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func (SkillURLSource) SkillSourceType() string { return "url" }

type SkillInfo struct {
	Name        string       `json:"name"`
	Description *string      `json:"description,omitempty"`
	Slash       *bool        `json:"slash,omitempty"`
	Location    AbsolutePath `json:"location"`
	Content     string       `json:"content"`
}

type SkillEmbeddedSource struct {
	Type  string    `json:"type"`
	Skill SkillInfo `json:"skill"`
}

func (SkillEmbeddedSource) SkillSourceType() string { return "embedded" }

// SkillSource mirrors the tagged Skill.Source union (directory | url | embedded).
type SkillSource interface {
	SkillSourceType() string
}

// SkillSourceEquals mirrors Skill.Source.equals.
func SkillSourceEquals(a, b SkillSource) bool {
	if a == nil || b == nil || a.SkillSourceType() != b.SkillSourceType() {
		return false
	}
	switch av := a.(type) {
	case SkillDirectorySource:
		bv, ok := b.(SkillDirectorySource)
		return ok && av.Path == bv.Path
	case SkillURLSource:
		bv, ok := b.(SkillURLSource)
		return ok && av.URL == bv.URL
	case SkillEmbeddedSource:
		bv, ok := b.(SkillEmbeddedSource)
		return ok && av.Skill.Name == bv.Skill.Name
	}
	return false
}

// SkillSourceKey mirrors Skill.Source.key.
func SkillSourceKey(source SkillSource) string {
	switch value := source.(type) {
	case SkillDirectorySource:
		return "directory:" + string(value.Path)
	case SkillURLSource:
		return "url:" + value.URL
	case SkillEmbeddedSource:
		return "embedded:" + value.Skill.Name
	}
	return ""
}
