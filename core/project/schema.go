package project

import "github.com/jiaweisky/go-agent-sdk/schema"

type ID = schema.ProjectID

type VcsType string

const VcsTypeGit VcsType = "git"

type Vcs struct {
	Type  VcsType
	Store schema.AbsolutePath
}
