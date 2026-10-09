package provider

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm"
	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
)

func mustProviderModel(t *testing.T, model func(string, ...llm.ModelOptions) (route.Model, error), id string) route.Model {
	t.Helper()
	resolved, err := model(id)
	if err != nil {
		t.Fatalf("model %q: %v", id, err)
	}
	return resolved
}
