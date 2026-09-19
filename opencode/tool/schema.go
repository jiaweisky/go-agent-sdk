package tool

import (
	opencodeid "github.com/user/go-agent-sdk/opencode/id"
)

type ToolID string

func (t ToolID) String() string {
	return string(t)
}

func NewToolID(given ...string) ToolID {
	return ToolID(opencodeid.Ascending("tool", given...))
}
