package schema

type ProjectVcs string

const ProjectVcsGit ProjectVcs = "git"

// LocationVcs is the version-control backing of a resolved location.
type LocationVcs struct {
	Type  ProjectVcs
	Store AbsolutePath
}

type ProjectIcon struct {
	URL      *string
	Override *string
	Color    *string
}

type ProjectCommands struct {
	Start *string
}

type ProjectTime struct {
	Created     NonNegativeInt
	Updated     NonNegativeInt
	Initialized *NonNegativeInt
}

type ProjectInfo struct {
	ID        ProjectID
	Worktree  string
	Vcs       *ProjectVcs
	Name      *string
	Icon      *ProjectIcon
	Commands  *ProjectCommands
	Time      ProjectTime
	Sandboxes []string
}

var ProjectEventUpdated = Define(DefineInput{
	Type: "project.updated",
	Schema: map[string]any{
		"id":        nil,
		"worktree":  nil,
		"vcs":       nil,
		"name":      nil,
		"icon":      nil,
		"commands":  nil,
		"time":      nil,
		"sandboxes": nil,
	},
})

var ProjectEvent = struct {
	Updated     EventDefinition
	Definitions []EventDefinition
}{
	Updated:     ProjectEventUpdated,
	Definitions: Inventory(ProjectEventUpdated),
}
