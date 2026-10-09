package project

import (
	core "github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// NewLocation resolves ref against proj and returns the SDK Location.
func NewLocation(ref schema.LocationRef, proj Project) (*core.Location, error) {
	resolved, err := proj.Resolve(ref.Directory)
	if err != nil {
		return nil, err
	}
	location := &core.Location{
		LocationInfo: schema.LocationInfo{
			Directory:   ref.Directory,
			WorkspaceID: ref.WorkspaceID,
			Project: schema.LocationProject{
				ID:        resolved.ID,
				Directory: resolved.Directory,
			},
		},
	}
	if resolved.Vcs != nil {
		location.Vcs = &schema.LocationVcs{Type: schema.ProjectVcs(resolved.Vcs.Type), Store: resolved.Vcs.Store}
	}
	return location, nil
}
