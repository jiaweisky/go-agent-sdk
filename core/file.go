package core

import "github.com/jiaweisky/go-agent-sdk/schema"

type FileDiffStatus string

const (
	FileDiffAdded    FileDiffStatus = "added"
	FileDiffModified FileDiffStatus = "modified"
	FileDiffDeleted  FileDiffStatus = "deleted"
)

type FileDiff struct {
	Path      schema.RelativePath
	Status    FileDiffStatus
	Additions schema.NonNegativeInt
	Deletions schema.NonNegativeInt
	Patch     string
}
