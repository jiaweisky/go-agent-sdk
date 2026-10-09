package schema

// ReferenceLocalSource is a reference from a local directory.
type ReferenceLocalSource struct {
	Type        string       `json:"type"`
	Path        AbsolutePath `json:"path"`
	Description *string      `json:"description,omitempty"`
	// Hidden is carried into ReferenceInfo; see ReferenceInfo.Hidden.
	Hidden *bool `json:"hidden,omitempty"`
}

func (ReferenceLocalSource) ReferenceSourceType() string { return "local" }

// ReferenceGitSource is a reference cloned from a git repository.
type ReferenceGitSource struct {
	Type        string  `json:"type"`
	Repository  string  `json:"repository"`
	Branch      *string `json:"branch,omitempty"`
	Description *string `json:"description,omitempty"`
	// Hidden is carried into ReferenceInfo; see ReferenceInfo.Hidden.
	Hidden *bool `json:"hidden,omitempty"`
}

func (ReferenceGitSource) ReferenceSourceType() string { return "git" }

// ReferenceSource is where a reference comes from: a local path or a git
// repository.
type ReferenceSource interface {
	ReferenceSourceType() string
}

// ReferenceInfo is a materialized reference.
type ReferenceInfo struct {
	Name        string       `json:"name"`
	Path        AbsolutePath `json:"path"`
	Description *string      `json:"description,omitempty"`
	// Hidden is informational: the SDK carries it through but does not act on
	// it. Reference guidance advertises a reference when it has a Description,
	// regardless of Hidden.
	Hidden *bool           `json:"hidden,omitempty"`
	Source ReferenceSource `json:"source"`
}

// ReferenceEventUpdated is published when the reference set changes.
var ReferenceEventUpdated = Define(DefineInput{
	Type:   "reference.updated",
	Schema: map[string]any{},
})

var ReferenceEvent = struct {
	Updated     EventDefinition
	Definitions []EventDefinition
}{
	Updated:     ReferenceEventUpdated,
	Definitions: Inventory(ReferenceEventUpdated),
}
