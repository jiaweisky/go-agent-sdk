package runner

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
)

func mustBindModel(t *testing.T, r route.Route, input route.RouteModelInput) route.Model {
	t.Helper()
	model, err := r.Model(input)
	if err != nil {
		t.Fatalf("route.Model(%s): %v", r.ID, err)
	}
	return model
}
