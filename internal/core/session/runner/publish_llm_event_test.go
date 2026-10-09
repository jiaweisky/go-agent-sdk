package runner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type capturedEvent struct {
	Type string
	Data any
}

type capturePublisher struct {
	published []capturedEvent
}

func (p *capturePublisher) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	eventType := def.Type
	if def.Durable != nil {
		eventType = schema.VersionedType(def.Type, def.Durable.Version)
	}
	p.published = append(p.published, capturedEvent{Type: eventType, Data: data})
	return &schema.EventPayloadDurable{Seq: len(p.published)}, nil
}

func capturePublisherForTest(t *testing.T) (*capturePublisher, *LLMEventPublisher) {
	t.Helper()
	publisher := &capturePublisher{}
	llm := NewLLMEventPublisher(publisher, LLMEventPublisherInput{
		SessionID: "ses_tool_event_test",
		Agent:     "build",
		Model:     schema.ModelRef{ID: "model", ProviderID: "provider"},
	})
	return publisher, llm
}

func versionedType(def schema.EventDefinition) string {
	if def.Durable == nil {
		return def.Type
	}
	return schema.VersionedType(def.Type, def.Durable.Version)
}

func findEvent(publisher *capturePublisher, eventType string) (capturedEvent, bool) {
	for _, event := range publisher.published {
		if event.Type == eventType {
			return event, true
		}
	}
	return capturedEvent{}, false
}

func eventData(t *testing.T, event capturedEvent) map[string]any {
	t.Helper()
	data, ok := event.Data.(map[string]any)
	if !ok {
		t.Fatalf("event data = %T", event.Data)
	}
	return data
}

func TestLLMEventPublisherLocalToolSuccessMedia(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	base64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"
	name := "pixel.png"
	content := []llmschema.ToolContent{
		llmschema.ToolTextContent{Type: "text", Text: "Image read successfully"},
		llmschema.ToolFileContent{Type: "file", URI: "data:image/png;base64," + base64, Mime: "image/png", Name: &name},
	}
	call := llmschema.NewToolCall("call-image", "read", map[string]any{"path": "pixel.png"})
	output := llmschema.ToolOutput{Structured: map[string]any{"type": "media", "mime": "image/png"}, Content: content}
	result := llmschema.NewToolResult("call-image", "read",
		llmschema.ToolResultValue{Type: "content", Value: content}, llmschema.WithEventOutput(&output))
	if err := llm.Publish(call, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if err := llm.Publish(result, nil); err != nil {
		t.Fatalf("result: %v", err)
	}

	success, ok := findEvent(publisher, versionedType(schema.SessionEventToolSuccess))
	if !ok {
		t.Fatalf("no success event: %+v", publisher.published)
	}
	serialized, _ := json.Marshal(success)
	if strings.Count(string(serialized), base64) != 1 {
		t.Fatalf("base64 serialized %d times", strings.Count(string(serialized), base64))
	}
	data := eventData(t, success)
	if _, hasResult := data["result"]; hasResult {
		t.Fatalf("success should not include result")
	}
	gotContent, _ := data["content"].([]llmschema.ToolContent)
	if len(gotContent) != 2 {
		t.Fatalf("content = %+v", data["content"])
	}
	if text, ok := gotContent[0].(llmschema.ToolTextContent); !ok || text.Text != "Image read successfully" {
		t.Fatalf("content[0] = %+v", gotContent[0])
	}
	if file, ok := gotContent[1].(llmschema.ToolFileContent); !ok || file.URI != "data:image/png;base64,"+base64 || file.Mime != "image/png" {
		t.Fatalf("content[1] = %+v", gotContent[1])
	}
}

func TestLLMEventPublisherProviderExecutedSuccess(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	call := llmschema.NewToolCall("call-image", "read", map[string]any{"path": "pixel.png"}, llmschema.WithEventProviderExecuted(true))
	result := llmschema.NewToolResult("call-image", "read",
		llmschema.ToolResultValue{Type: "content", Value: []llmschema.ToolContent{}}, llmschema.WithEventProviderExecuted(true))
	if err := llm.Publish(call, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if err := llm.Publish(result, nil); err != nil {
		t.Fatalf("result: %v", err)
	}
	success, ok := findEvent(publisher, versionedType(schema.SessionEventToolSuccess))
	if !ok {
		t.Fatalf("no success event")
	}
	if _, hasResult := eventData(t, success)["result"]; !hasResult {
		t.Fatalf("provider-executed success should include result")
	}
}

func TestLLMEventPublisherBinaryFailure(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	call := llmschema.NewToolCall("call-image", "read", map[string]any{"path": "pixel.png"})
	result := llmschema.NewToolResult("call-image", "read",
		llmschema.ToolResultValue{Type: "error", Value: "Cannot read binary file"})
	if err := llm.Publish(call, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if err := llm.Publish(result, nil); err != nil {
		t.Fatalf("result: %v", err)
	}
	if _, ok := findEvent(publisher, versionedType(schema.SessionEventToolSuccess)); ok {
		t.Fatalf("unexpected success event")
	}
	if _, ok := findEvent(publisher, versionedType(schema.SessionEventToolFailed)); !ok {
		t.Fatalf("expected failed event")
	}
}

func TestLLMEventPublisherStepFinishSettlement(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	if err := llm.Publish(llmschema.NewStepStart(0), nil); err != nil {
		t.Fatalf("step start: %v", err)
	}
	if err := llm.Publish(llmschema.NewStepFinish(0, llmschema.ReasonStop), nil); err != nil {
		t.Fatalf("step finish: %v", err)
	}
	if _, ok := findEvent(publisher, versionedType(schema.SessionEventStepEnded)); ok {
		t.Fatalf("step finish should not publish step ended")
	}
	settlement := llm.StepSettlement()
	if settlement == nil || settlement.Finish != "stop" {
		t.Fatalf("settlement = %+v", settlement)
	}
}

func TestLLMEventPublisherTextFragments(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	if err := llm.Publish(llmschema.NewTextStart("t1"), nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := llm.Publish(llmschema.NewTextDelta("t1", "Hel"), nil); err != nil {
		t.Fatalf("delta: %v", err)
	}
	if err := llm.Publish(llmschema.NewTextDelta("t1", "lo"), nil); err != nil {
		t.Fatalf("delta: %v", err)
	}
	if err := llm.Publish(llmschema.NewTextEnd("t1"), nil); err != nil {
		t.Fatalf("end: %v", err)
	}
	ended, ok := findEvent(publisher, versionedType(schema.SessionEventTextEnded))
	if !ok {
		t.Fatalf("no text ended event")
	}
	if text := eventData(t, ended)["text"]; text != "Hello" {
		t.Fatalf("text = %v", text)
	}
}

func TestLLMEventPublisherFlushEndsOpenFragments(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	if err := llm.Publish(llmschema.NewTextStart("t1"), nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := llm.Publish(llmschema.NewTextDelta("t1", "partial"), nil); err != nil {
		t.Fatalf("delta: %v", err)
	}
	if err := llm.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	ended, ok := findEvent(publisher, versionedType(schema.SessionEventTextEnded))
	if !ok || eventData(t, ended)["text"] != "partial" {
		t.Fatalf("flushed text = %+v", ended)
	}
}

func TestLLMEventPublisherProviderErrorFailsAssistant(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	if err := llm.Publish(llmschema.NewProviderErrorEvent("boom"), nil); err != nil {
		t.Fatalf("provider error: %v", err)
	}
	if !llm.HasProviderError() {
		t.Fatalf("expected provider error")
	}
	failed, ok := findEvent(publisher, versionedType(schema.SessionEventStepFailed))
	if !ok {
		t.Fatalf("expected step failed event")
	}
	if errorValue, ok := eventData(t, failed)["error"].(schema.SessionMessageUnknownError); !ok || errorValue.Message != "boom" {
		t.Fatalf("error = %+v", eventData(t, failed)["error"])
	}
}

func TestLLMEventPublisherFailUnsettledToolsOrder(t *testing.T) {
	publisher, llm := capturePublisherForTest(t)
	for _, id := range []string{"call-a", "call-b", "call-c"} {
		if err := llm.Publish(llmschema.NewToolInputStart(id, "read"), nil); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}
	if err := llm.FailUnsettledTools("interrupted", false); err != nil {
		t.Fatalf("fail: %v", err)
	}
	var failed []string
	for _, event := range publisher.published {
		if event.Type != versionedType(schema.SessionEventToolFailed) {
			continue
		}
		failed = append(failed, eventData(t, event)["callID"].(string))
	}
	if want := []string{"call-a", "call-b", "call-c"}; !reflect.DeepEqual(failed, want) {
		t.Fatalf("failed = %v want %v", failed, want)
	}

	// hostedOnly skips tools that were not provider-executed.
	hostedPublisher, hostedLLM := capturePublisherForTest(t)
	if err := hostedLLM.Publish(llmschema.NewToolInputStart("call-local", "read"), nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := hostedLLM.FailUnsettledTools("interrupted", true); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if _, ok := findEvent(hostedPublisher, versionedType(schema.SessionEventToolFailed)); ok {
		t.Fatalf("hostedOnly should skip non-provider tools")
	}
}
