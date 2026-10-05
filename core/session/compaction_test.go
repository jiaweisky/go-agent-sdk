package session

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/llm"
	"github.com/jiaweisky/go-agent-sdk/llm/route"
	llmschema "github.com/jiaweisky/go-agent-sdk/llm/schema"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

type compactionTestPublisher struct {
	published []string
	ended     map[string]any
}

func (p *compactionTestPublisher) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	p.published = append(p.published, def.Type)
	if def.Type == schema.SessionEventCompactionEnded.Type {
		p.ended, _ = data.(map[string]any)
	}
	return &schema.EventPayloadDurable{Seq: len(p.published)}, nil
}

func compactionTestModel(context, output int) route.Model {
	return route.Model{
		Model: llmschema.Model{ID: "test-model", Provider: "test"},
		Route: route.Route{Defaults: route.RouteDefaults{
			Limits: &llmschema.ModelLimits{Context: context, Output: output},
		}},
	}
}

func compactionTestRequest(model llm.ModelSchema, maxTokens *int) llm.LLMRequest {
	return *llm.NewRequest(llm.RequestInput{
		Model:      model,
		Generation: &llm.GenerationOptions{MaxTokens: maxTokens},
	})
}

func compactionTextLLM(text string) func(context.Context, llm.LLMRequest) iter.Seq2[llm.LLMEvent, error] {
	return func(_ context.Context, request llm.LLMRequest) iter.Seq2[llm.LLMEvent, error] {
		return func(yield func(llm.LLMEvent, error) bool) {
			yield(llmschema.NewTextDelta("t1", text), nil)
		}
	}
}

func userEntry(seq int64, text string) CompactionEntry {
	return CompactionEntry{Seq: seq, Message: schema.SessionMessageUser{ID: "msg", Text: text, Type: "user"}}
}

func TestBuildCompactionPromptNewSummary(t *testing.T) {
	prompt := BuildCompactionPrompt(CompactionPromptInput{Context: []string{"conversation history"}})

	if !strings.HasPrefix(prompt, "Here is the conversation so far:\n\n<conversation>\nconversation history\n</conversation>") {
		t.Fatalf("prompt = %q", prompt)
	}
	if strings.Index(prompt, "</conversation>") >= strings.Index(prompt, "Create a new anchored summary") {
		t.Fatalf("conversation should precede the instruction")
	}
	for _, want := range []string{
		"conversation history in the <conversation> tags above",
		"## Work State\n### Completed",
		"### Active",
		"### Blocked",
		"## Relevant Files",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestBuildCompactionPromptPriorSummary(t *testing.T) {
	prompt := BuildCompactionPrompt(CompactionPromptInput{Context: []string{"new conversation"}, PreviousSummary: "existing summary"})

	if strings.Index(prompt, "<conversation>") >= strings.Index(prompt, "<prior-summary>") {
		t.Fatalf("conversation should precede prior-summary")
	}
	if strings.Index(prompt, "</prior-summary>") >= strings.Index(prompt, "The <prior-summary> summarizes") {
		t.Fatalf("prior-summary should precede the update instructions")
	}
	for _, want := range []string{
		"Carry forward objectives, constraints, user directives, decisions, and parallel workstreams from the <prior-summary>",
		`Move completed work from "Active" to "Completed".`,
		`Update "Objective" and "Next Move" to reflect the current work state.`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestSerializeToolContentDescribesMedia(t *testing.T) {
	base64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"
	name := "pixel.png"
	serialized := SerializeToolContent([]schema.ToolContent{
		schema.ToolTextContent{Type: "text", Text: "Image read successfully"},
		schema.ToolFileContent{Type: "file", URI: "data:image/png;base64," + base64, Mime: "image/png", Name: &name},
	})
	if serialized != "Image read successfully\n[Attached image/png: pixel.png]" {
		t.Fatalf("serialized = %q", serialized)
	}
	if strings.Contains(serialized, base64) {
		t.Fatalf("serialized should not embed base64")
	}
}

func TestDefaultCompactionSettings(t *testing.T) {
	defaults := DefaultCompactionSettings()
	if !defaults.Auto || defaults.Buffer != compactionDefaultBuffer || defaults.Tokens != compactionDefaultKeepTokens {
		t.Fatalf("defaults = %+v", defaults)
	}
}

func TestCompactionSelectSplitsOnTokenBudget(t *testing.T) {
	large := userEntry(0, strings.Repeat("a", 400))
	small := userEntry(1, "bb")
	selected, ok := compactionSelect([]CompactionEntry{large, small}, 50)
	if !ok {
		t.Fatalf("expected selection")
	}
	if !strings.HasPrefix(selected.Head, "[User]: aaa") || strings.Contains(selected.Head, "bb") {
		t.Fatalf("head = %q", selected.Head)
	}
	if selected.Recent != "[User]: bb" {
		t.Fatalf("recent = %q", selected.Recent)
	}

	if _, ok := compactionSelect([]CompactionEntry{{Seq: 0, Message: schema.SessionMessageCompaction{Type: "compaction"}}}, 50); ok {
		t.Fatalf("expected no selection for compaction-only entries")
	}
}

func TestCompactionCompactAfterOverflowPublishesSummary(t *testing.T) {
	model := compactionTestModel(100_000, 1_000)
	publisher := &compactionTestPublisher{}
	compaction := NewCompaction(CompactionDependencies{
		Events:   publisher,
		LLM:      compactionTextLLM("summary text"),
		Settings: DefaultCompactionSettings(),
	})
	input := CompactionInput{
		SessionID: "ses_1",
		Entries: []CompactionEntry{
			{Seq: 0, Message: schema.SessionMessageCompaction{ID: "msg_c", Type: "compaction", Summary: "old", Recent: "recent ctx"}},
			userEntry(1, "hello"),
		},
		Model:   model,
		Request: compactionTestRequest(model, compactionIntPtr(1_000)),
	}
	compacted, err := compaction.CompactAfterOverflow(input)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !compacted {
		t.Fatalf("expected compaction")
	}
	if len(publisher.published) != 2 ||
		publisher.published[0] != schema.SessionEventCompactionStarted.Type ||
		publisher.published[1] != schema.SessionEventCompactionEnded.Type {
		t.Fatalf("published = %v", publisher.published)
	}
	if publisher.ended["text"] != "summary text" || publisher.ended["recent"] != "[User]: hello" {
		t.Fatalf("ended = %+v", publisher.ended)
	}
}

func TestCompactionCompactAfterOverflowSkipsWhenNoContextLimit(t *testing.T) {
	model := compactionTestModel(0, 0)
	publisher := &compactionTestPublisher{}
	compaction := NewCompaction(CompactionDependencies{Events: publisher, LLM: compactionTextLLM("x")})
	compacted, err := compaction.CompactAfterOverflow(CompactionInput{
		SessionID: "ses_1", Entries: []CompactionEntry{userEntry(0, "hi")}, Model: model,
	})
	if err != nil || compacted {
		t.Fatalf("compacted = %v err = %v", compacted, err)
	}
	if len(publisher.published) != 0 {
		t.Fatalf("published = %v", publisher.published)
	}
}

func TestCompactionCompactOnDemandWithoutLimit(t *testing.T) {
	model := compactionTestModel(0, 0)
	publisher := &compactionTestPublisher{}
	compaction := NewCompaction(CompactionDependencies{
		Events:   publisher,
		LLM:      compactionTextLLM("manual summary"),
		Settings: CompactionSettings{Auto: false, Tokens: 1},
	})
	compacted, err := compaction.Compact(CompactionInput{
		SessionID: "ses_1",
		Entries:   []CompactionEntry{userEntry(1, "hello")},
		Model:     model,
	})
	if err != nil || !compacted {
		t.Fatalf("compacted = %v err = %v", compacted, err)
	}
	if publisher.ended["text"] != "manual summary" || publisher.ended["reason"] != "manual" {
		t.Fatalf("ended = %+v", publisher.ended)
	}
}

func TestCompactionCompactIfNeeded(t *testing.T) {
	publisher := &compactionTestPublisher{}
	compaction := NewCompaction(CompactionDependencies{
		Events:   publisher,
		LLM:      compactionTextLLM("summary"),
		Settings: DefaultCompactionSettings(),
	})
	entries := []CompactionEntry{
		{Seq: 0, Message: schema.SessionMessageCompaction{ID: "msg_c", Type: "compaction", Summary: "old", Recent: "recent"}},
		userEntry(1, "hello"),
	}

	// Auto disabled.
	disabled := false
	off := NewCompaction(CompactionDependencies{
		Events:   publisher,
		LLM:      compactionTextLLM("summary"),
		Settings: CompactionSettings{Auto: disabled},
	})
	if compacted, err := off.CompactIfNeeded(CompactionInput{SessionID: "ses_1", Entries: entries, Model: compactionTestModel(1000, 100)}); err != nil || compacted {
		t.Fatalf("disabled: compacted=%v err=%v", compacted, err)
	}

	// Under budget.
	large := compactionTestModel(1_000_000, 100)
	if compacted, err := compaction.CompactIfNeeded(CompactionInput{SessionID: "ses_1", Entries: entries, Model: large}); err != nil || compacted {
		t.Fatalf("under budget: compacted=%v err=%v", compacted, err)
	}

	// Overflow triggers compaction.
	small := compactionTestModel(1_000, 100)
	if compacted, err := compaction.CompactIfNeeded(CompactionInput{SessionID: "ses_1", Entries: entries, Model: small}); err != nil || !compacted {
		t.Fatalf("overflow: compacted=%v err=%v", compacted, err)
	}
	if len(publisher.published) != 2 {
		t.Fatalf("published = %v", publisher.published)
	}
}

func TestCompactionSerialize(t *testing.T) {
	name := "diagram.png"
	user := schema.SessionMessageUser{Type: "user", Text: "question", Files: []schema.PromptFileAttachment{
		{URI: "data:image/png;base64,abc", Mime: "image/png", Name: &name},
	}}
	if got, want := compactionSerialize(user), "[User]: question\n[Attached image/png: diagram.png]"; got != want {
		t.Fatalf("user = %q want %q", got, want)
	}

	assistant := schema.SessionMessageAssistant{Type: "assistant", Content: []schema.SessionMessageAssistantContent{
		schema.SessionMessageAssistantText{Type: "text", Text: "hi"},
		schema.SessionMessageAssistantReasoning{Type: "reasoning", Text: "think"},
		schema.SessionMessageAssistantTool{Type: "tool", Name: "read", State: schema.SessionMessageToolStateCompleted{
			Status: "completed", Input: map[string]any{"path": "a"},
			Content: []schema.ToolContent{schema.ToolTextContent{Type: "text", Text: "out"}},
		}},
		schema.SessionMessageAssistantTool{Type: "tool", Name: "bad", State: schema.SessionMessageToolStateError{
			Status: "error", Input: map[string]any{"path": "b"},
			Error: schema.SessionMessageUnknownError{Type: "x", Message: "boom"},
		}},
		schema.SessionMessageAssistantTool{Type: "tool", Name: "wait", State: schema.SessionMessageToolStatePending{
			Status: "pending", Input: `{"path":"c"}`,
		}},
	}}
	want := strings.Join([]string{
		"[Assistant]: hi",
		"[Assistant reasoning]: think",
		`[Assistant tool call]: read({"path":"a"})`,
		"[Tool result]: out",
		`[Assistant tool call]: bad({"path":"b"})`,
		"[Tool error]: boom",
		`[Assistant tool call]: wait({"path":"c"})`,
	}, "\n")
	if got := compactionSerialize(assistant); got != want {
		t.Fatalf("assistant = %q want %q", got, want)
	}

	cases := []struct {
		message schema.SessionMessage
		want    string
	}{
		{schema.SessionMessageSystem{Type: "system", Text: "s"}, "[System update]: s"},
		{schema.SessionMessageSynthetic{Type: "synthetic", Text: "y"}, "[Synthetic context]: y"},
		{schema.SessionMessageShell{Type: "shell", Command: "ls", Output: "files"}, "[Shell]: ls\nfiles"},
		{schema.SessionMessageAgentSwitched{Type: "agent-switched"}, ""},
	}
	for _, test := range cases {
		if got := compactionSerialize(test.message); got != test.want {
			t.Fatalf("serialize(%T) = %q want %q", test.message, got, test.want)
		}
	}
}

func TestCompactionTruncate(t *testing.T) {
	if got := compactionTruncate("short"); got != "short" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("a", compactionToolOutputMaxChars+10)
	want := strings.Repeat("a", compactionToolOutputMaxChars) + "\n[truncated]"
	if got := compactionTruncate(long); got != want {
		t.Fatalf("long = %q", got)
	}
}

func TestCompactionCompactAfterOverflowHandlesLLMFailure(t *testing.T) {
	model := compactionTestModel(100_000, 1_000)
	entries := []CompactionEntry{
		{Seq: 0, Message: schema.SessionMessageCompaction{ID: "msg_c", Type: "compaction", Summary: "old", Recent: "recent"}},
		userEntry(1, "hello"),
	}
	input := CompactionInput{SessionID: "ses_1", Entries: entries, Model: model}

	failing := func(err error) func(context.Context, llm.LLMRequest) iter.Seq2[llm.LLMEvent, error] {
		return func(context.Context, llm.LLMRequest) iter.Seq2[llm.LLMEvent, error] {
			return func(yield func(llm.LLMEvent, error) bool) { yield(llm.LLMEvent{}, err) }
		}
	}
	providerFailure := func(context.Context, llm.LLMRequest) iter.Seq2[llm.LLMEvent, error] {
		return func(yield func(llm.LLMEvent, error) bool) {
			yield(llmschema.NewProviderErrorEvent("boom"), nil)
		}
	}

	for name, stream := range map[string]func(context.Context, llm.LLMRequest) iter.Seq2[llm.LLMEvent, error]{
		"llm error":      failing(&llm.LLMError{Module: "test", Method: "stream"}),
		"provider error": providerFailure,
	} {
		publisher := &compactionTestPublisher{}
		compaction := NewCompaction(CompactionDependencies{Events: publisher, LLM: stream})
		compacted, err := compaction.CompactAfterOverflow(input)
		if err != nil || compacted {
			t.Fatalf("%s: compacted=%v err=%v", name, compacted, err)
		}
		if len(publisher.published) != 1 || publisher.published[0] != schema.SessionEventCompactionStarted.Type {
			t.Fatalf("%s: published = %v", name, publisher.published)
		}
	}
}

func TestCompactionCompactIfNeededEstimatesRequest(t *testing.T) {
	publisher := &compactionTestPublisher{}
	compaction := NewCompaction(CompactionDependencies{
		Events:   publisher,
		LLM:      compactionTextLLM("summary"),
		Settings: DefaultCompactionSettings(),
	})
	entries := []CompactionEntry{
		{Seq: 0, Message: schema.SessionMessageCompaction{ID: "msg_c", Type: "compaction", Summary: "old", Recent: "recent"}},
		userEntry(1, "hello"),
	}
	// context 100_000, output 90_000 -> threshold 10_000 tokens.
	model := compactionTestModel(100_000, 90_000)

	small := *llm.NewRequest(llm.RequestInput{
		Model:    model,
		Messages: []llm.Message{llmschema.MessageUser("small")},
	})
	if compacted, err := compaction.CompactIfNeeded(CompactionInput{
		SessionID: "ses_1", Entries: entries, Model: model, Request: small,
	}); err != nil || compacted {
		t.Fatalf("under budget: compacted=%v err=%v", compacted, err)
	}

	large := *llm.NewRequest(llm.RequestInput{
		Model:    model,
		Messages: []llm.Message{llmschema.MessageUser(strings.Repeat("a", 50_000))},
	})
	if compacted, err := compaction.CompactIfNeeded(CompactionInput{
		SessionID: "ses_1", Entries: entries, Model: model, Request: large,
	}); err != nil || !compacted {
		t.Fatalf("over budget: compacted=%v err=%v", compacted, err)
	}
	if len(publisher.published) != 2 {
		t.Fatalf("published = %v", publisher.published)
	}
}

func compactionIntPtr(value int) *int { return &value }
