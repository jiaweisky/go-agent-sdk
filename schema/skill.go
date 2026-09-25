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

// SkillSource is where a skill comes from: a directory, a URL or an embedded
// value.
type SkillSource interface {
	SkillSourceType() string
}

// SkillSourceEquals reports whether two sources refer to the same skill.
func SkillSourceEquals(a, b SkillSource) bool {
	if a == nil || b == nil {
		return false
	}
	keyA, keyB := SkillSourceKey(a), SkillSourceKey(b)
	return keyA != "" && keyA == keyB
}

// SkillSourceKey is the identity of a skill source.
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
