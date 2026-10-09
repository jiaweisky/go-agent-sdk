package core

import (
	"github.com/jiaweisky/go-agent-sdk/core/project"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type Location struct {
	schema.LocationInfo
	Vcs *project.Vcs
}

func NewLocation(ref schema.LocationRef, proj Project) (*Location, error) {
	resolved, err := proj.Resolve(ref.Directory)
	if err != nil {
		return nil, err
	}
	return &Location{
		LocationInfo: schema.LocationInfo{
			Directory:   ref.Directory,
			WorkspaceID: ref.WorkspaceID,
			Project: schema.LocationProject{
				ID:        resolved.ID,
				Directory: resolved.Directory,
			},
		},
		Vcs: resolved.Vcs,
	}, nil
}
