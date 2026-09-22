package schema

// ReferenceLocalSource mirrors Reference.LocalSource.
type ReferenceLocalSource struct {
	Type        string       `json:"type"`
	Path        AbsolutePath `json:"path"`
	Description *string      `json:"description,omitempty"`
	Hidden      *bool        `json:"hidden,omitempty"`
}

func (ReferenceLocalSource) ReferenceSourceType() string { return "local" }

// ReferenceGitSource mirrors Reference.GitSource.
type ReferenceGitSource struct {
	Type        string  `json:"type"`
	Repository  string  `json:"repository"`
	Branch      *string `json:"branch,omitempty"`
	Description *string `json:"description,omitempty"`
	Hidden      *bool   `json:"hidden,omitempty"`
}

func (ReferenceGitSource) ReferenceSourceType() string { return "git" }

// ReferenceSource mirrors the tagged Reference.Source union.
type ReferenceSource interface {
	ReferenceSourceType() string
}

// ReferenceInfo mirrors Reference.Info.
type ReferenceInfo struct {
	Name        string          `json:"name"`
	Path        AbsolutePath    `json:"path"`
	Description *string         `json:"description,omitempty"`
	Hidden      *bool           `json:"hidden,omitempty"`
	Source      ReferenceSource `json:"source"`
}

// ReferenceEventUpdated mirrors Reference.Event.Updated.
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
