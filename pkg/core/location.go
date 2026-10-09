package core

import "github.com/jiaweisky/go-agent-sdk/pkg/schema"

// Location is a resolved working location: the project it belongs to and, when
// version-controlled, the VCS backing it.
type Location struct {
	schema.LocationInfo
	Vcs *schema.LocationVcs
}
