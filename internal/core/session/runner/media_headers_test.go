package runner

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/route"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestMediaFileHeadersOnlyWithFileRef(t *testing.T) {
	model := route.Model{
		Model: llmschema.Model{Provider: "p"},
		Route: route.Route{Defaults: route.RouteDefaults{Headers: map[string]string{"X-Model": "model-wins"}}},
	}
	runner := &LLMRunner{MediaFileHeaders: map[schema.ProviderID]map[string]string{
		"p": {"anthropic-beta": "files", "X-Model": "profile-loses"},
	}}
	if got := runner.mediaFileHeadersFor(model, nil); got != nil {
		t.Fatalf("headers without a file reference = %#v, want nil", got)
	}
	withoutFile := []schema.SessionMessage{schema.SessionMessageUser{Text: "hi"}}
	if got := runner.mediaFileHeadersFor(model, withoutFile); got != nil {
		t.Fatalf("headers without a file reference = %#v, want nil", got)
	}
	withFile := []schema.SessionMessage{schema.SessionMessageUser{
		Files: []schema.PromptFileAttachment{{FileID: "file-1", MediaSource: "file"}},
	}}
	got := runner.mediaFileHeadersFor(model, withFile)
	if got["anthropic-beta"] != "files" {
		t.Fatalf("profile header missing: %#v", got)
	}
	if _, ok := got["X-Model"]; ok {
		t.Fatalf("model header should win over the profile: %#v", got)
	}

	toolFile := []schema.SessionMessage{schema.SessionMessageAssistant{
		Content: []schema.SessionMessageAssistantContent{schema.SessionMessageAssistantTool{
			State: schema.SessionMessageToolStateCompleted{
				Content: schema.ToolContents{schema.ToolFileContent{FileID: "file-2"}},
			},
		}},
	}}
	if got := runner.mediaFileHeadersFor(model, toolFile); got["anthropic-beta"] != "files" {
		t.Fatalf("tool-result file header missing: %#v", got)
	}
}
