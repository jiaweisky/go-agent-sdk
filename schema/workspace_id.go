package schema

import (
	"fmt"
	"strings"
)

type WorkspaceID string

func (id WorkspaceID) String() string {
	return string(id)
}

func NewWorkspaceID(id ...string) WorkspaceID {
	if len(id) == 0 || id[0] == "" {
		return WorkspaceID("wrk_" + Ascending())
	}
	if !strings.HasPrefix(id[0], "wrk") {
		panic(fmt.Sprintf("ID %s does not start with wrk", id[0]))
	}
	return WorkspaceID(id[0])
}
