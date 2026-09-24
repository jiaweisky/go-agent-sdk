package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/go-agent-sdk/core"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

type fakeLLM struct{ reply string }

func (f fakeLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f fakeLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f fakeLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	events := []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", f.reply),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop),
		llmschema.NewFinish(llmschema.ReasonStop),
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

// blockingLLM blocks inside Stream until release is closed, so a test can
// observe a turn that is still in flight.
type blockingLLM struct {
	started chan struct{}
	release chan struct{}
	once    *sync.Once
}

func (b blockingLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (b blockingLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (b blockingLLM) Stream(ctx context.Context, req *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return func(yield func(llmschema.LLMEvent, error) bool) {
		b.once.Do(func() { close(b.started) })
		<-b.release
		if err := ctx.Err(); err != nil {
			yield(llmschema.LLMEvent{}, err)
			return
		}
		for _, event := range textEvents("done") {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

// manyDeltasLLM streams n text deltas in one text part, so a test can exercise
// event delivery under load.
type manyDeltasLLM struct{ deltas int }

func (m manyDeltasLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (m manyDeltasLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (m manyDeltasLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return func(yield func(llmschema.LLMEvent, error) bool) {
		if !yield(llmschema.NewStepStart(0), nil) {
			return
		}
		if !yield(llmschema.NewTextStart("t1"), nil) {
			return
		}
		for i := 0; i < m.deltas; i++ {
			if !yield(llmschema.NewTextDelta("t1", "x"), nil) {
				return
			}
		}
		for _, event := range []llmschema.LLMEvent{
			llmschema.NewTextEnd("t1"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
			llmschema.NewFinish(llmschema.ReasonStop),
		} {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("test"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        fakeLLM{reply: "hello world"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	return client
}

func TestClientOpenAICompatibleProvider(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Model: "deepseek-chat",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			info, err := NewModelInfo("openai-compatible-chat", id, "https://api.deepseek.com/v1", schema.ModelLimit{})
			if err != nil {
				return info, err
			}
			info.ProviderID = "deepseek"
			return info, nil
		},
		Credentials: APIKey("test"),
		DataDir:     t.TempDir(),
		LLM:         fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
}

type toolCallLLM struct {
	calls    *int
	toolName string
	reply    string
}

func (f toolCallLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f toolCallLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f toolCallLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	*f.calls++
	var events []llmschema.LLMEvent
	if *f.calls == 1 {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewToolCall("call-1", f.toolName, map[string]any{}),
			llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
			llmschema.NewFinish(llmschema.ReasonToolCalls),
		}
	} else {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewTextStart("t1"),
			llmschema.NewTextDelta("t1", f.reply),
			llmschema.NewTextEnd("t1"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
			llmschema.NewFinish(llmschema.ReasonStop),
		}
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func TestSessionPermissionHandlerIsWired(t *testing.T) {
	calls := 0
	permissionCalls := 0
	executed := false
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:   "test-model",
		DataDir: t.TempDir(),
		Permission: func(request PermissionRequest) (PermissionDecision, error) {
			permissionCalls++
			return PermissionDecision{Allow: true}, nil
		},
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{
		System:      "s",
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	echo := DefineTool("echo", "echo", func(struct{}, ToolContext) (string, error) {
		executed = true
		return "ok", nil
	})
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{echo}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			break // a denied tool interrupts the turn
		}
	}
	if !executed {
		t.Fatal("tool was not executed")
	}
	if permissionCalls == 0 {
		t.Fatal("permission handler was not called")
	}
}

func TestToolOutputIsBounded(t *testing.T) {
	calls := 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        toolCallLLM{calls: &calls, toolName: "big", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	big := DefineTool("big", "big", func(struct{}, ToolContext) (string, error) {
		return strings.Repeat("x", 200_000), nil
	})
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{big}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	messages, err := session.Messages()
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	var found bool
	for _, message := range messages {
		assistant, ok := message.(schema.SessionMessageAssistant)
		if !ok {
			continue
		}
		for _, item := range assistant.Content {
			toolPart, ok := item.(schema.SessionMessageAssistantTool)
			if !ok {
				continue
			}
			state, ok := toolPart.State.(schema.SessionMessageToolStateCompleted)
			if !ok {
				continue
			}
			found = true
			for _, content := range state.Content {
				text, ok := content.(schema.ToolTextContent)
				if !ok {
					continue
				}
				if len(text.Text) > 60_000 {
					t.Fatalf("tool output not bounded: %d bytes", len(text.Text))
				}
				if !strings.Contains(text.Text, "output truncated") {
					t.Fatalf("missing truncation marker: %q", text.Text[:min(80, len(text.Text))])
				}
			}
		}
	}
	if !found {
		t.Fatal("no completed tool result found")
	}
}

func TestSendContextCancelDoesNotInterruptTurn(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        blockingLLM{started: started, release: release, once: &once},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send did not stop waiting after ctx cancellation")
	}

	// Cancelling Send's ctx must not interrupt the turn: releasing the provider
	// still completes it and persists the assistant text (TS run-coordinator
	// semantics: only Abort interrupts).
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if lastAssistantText(t, session) == "done" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("turn was interrupted or did not complete; last text = %q", lastAssistantText(t, session))
}

func lastAssistantText(t *testing.T, session *Session) string {
	t.Helper()
	messages, err := session.Messages()
	if err != nil {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		assistant, ok := messages[i].(schema.SessionMessageAssistant)
		if !ok {
			continue
		}
		for j := len(assistant.Content) - 1; j >= 0; j-- {
			if text, ok := assistant.Content[j].(schema.SessionMessageAssistantText); ok {
				return text.Text
			}
		}
	}
	return ""
}

func TestSystemPromptCustomize(t *testing.T) {
	base := "base"
	info := &schema.AgentInfo{System: &base}
	applySystemPrompt(info, &SystemPromptConfig{
		Mode:    SystemPromptCustomize,
		Content: "extra",
		Sections: map[string]SectionOverride{
			SectionAgentSystem: {Action: SectionReplace, Content: "REPLACED"},
			"guidelines":       {Action: SectionReplace, Content: "guide"},
			"tone":             {Action: SectionAppend, Content: "be nice"},
		},
	})
	want := "REPLACED\n\nguide\n\nbe nice\n\nextra"
	if info.System == nil || *info.System != want {
		t.Fatalf("customize system = %q, want %q", valueOr(info.System), want)
	}

	replace := &schema.AgentInfo{System: &base}
	applySystemPrompt(replace, &SystemPromptConfig{Mode: SystemPromptReplace, Content: "only"})
	if replace.System == nil || *replace.System != "only" {
		t.Fatalf("replace system = %q", valueOr(replace.System))
	}

	appendInfo := &schema.AgentInfo{System: &base}
	applySystemPrompt(appendInfo, &SystemPromptConfig{Content: "more"})
	if appendInfo.System == nil || *appendInfo.System != "base\n\nmore" {
		t.Fatalf("append system = %q", valueOr(appendInfo.System))
	}
}

func valueOr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestSessionPermissionOverride(t *testing.T) {
	calls := 0
	executed := false
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:   "test-model",
		DataDir: t.TempDir(),
		// Client default denies; the session override allows.
		Permission: func(PermissionRequest) (PermissionDecision, error) {
			return PermissionDecision{Allow: false}, nil
		},
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{
		System:      "s",
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	echo := DefineTool("echo", "echo", func(struct{}, ToolContext) (string, error) {
		executed = true
		return "ok", nil
	})
	session, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent: "build",
		Tools: []Tool{echo},
		OnPermissionRequest: func(PermissionRequest) (PermissionDecision, error) {
			return PermissionDecision{Allow: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if !executed {
		t.Fatal("session permission override was not applied")
	}
}

func TestSessionMessages(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()
	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	messages, err := session.Messages()
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected stored messages")
	}
}

func TestModelResolverCarriesLimits(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Model:    "default-model",
		Protocol: "openai-compatible-chat",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			return NewModelInfo("openai-compatible-chat", id, "https://api.test/v1", schema.ModelLimit{Context: 1_000_000, Output: 32_000})
		},
	})
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	model, err := resolve(schema.SessionInfo{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	limits := model.Route.Defaults.Limits
	if limits == nil || limits.Context != 1_000_000 || limits.Output != 32_000 {
		t.Fatalf("limits = %+v, want context=1000000 output=32000", limits)
	}
}

func TestModelResolverUsesSessionModel(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Model:    "default-model",
		Protocol: "openai-compatible-chat",
		ModelInfo: func(id string) (schema.ModelInfo, error) {
			return NewModelInfo("openai-compatible-chat", id, "https://api.test/v1", schema.ModelLimit{})
		},
	})
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}
	model, err := resolve(schema.SessionInfo{Model: &schema.ModelRef{ID: "session-model"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if model.ID != "session-model" {
		t.Fatalf("model id = %q", model.ID)
	}
	fallback, err := resolve(schema.SessionInfo{})
	if err != nil {
		t.Fatalf("resolve fallback: %v", err)
	}
	if fallback.ID != "default-model" {
		t.Fatalf("fallback model id = %q", fallback.ID)
	}
}

func TestSessionSend(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var text string
	var idle bool
	for event, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		switch e := event.(type) {
		case TextDelta:
			text += e.Delta
		case Idle:
			idle = true
		}
	}
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
	if !idle {
		t.Fatal("expected Idle event")
	}
}

// TestSessionSendDoesNotDropEvents guards the backpressure handoff: a consumer
// slower than the producer must still see every delta, so the buffer only
// smooths bursts and never drops.
func TestSessionSendDoesNotDropEvents(t *testing.T) {
	const deltas = 600
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        manyDeltasLLM{deltas: deltas},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seen := 0
	for event, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if _, ok := event.(TextDelta); ok {
			seen++
			time.Sleep(time.Millisecond)
		}
	}
	if seen != deltas {
		t.Fatalf("received %d text deltas, want %d (events were dropped)", seen, deltas)
	}
}

func TestSessionOn(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()

	var observed int
	unsubscribe := session.On(func(event Event) {
		if _, ok := event.(TextDelta); ok {
			observed++
		}
	})
	defer unsubscribe()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if observed == 0 {
		t.Fatal("expected On handler to observe a TextDelta")
	}
}

func TestSessionListResumeDelete(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	session, err := client.CreateSession(ctx, &SessionConfig{Agent: "build", Title: "one"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	infos, err := client.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != session.ID() || infos[0].Title != "one" {
		t.Fatalf("infos = %+v", infos)
	}

	resumed, err := client.ResumeSession(ctx, session.ID(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer resumed.Close()
	if resumed.ID() != session.ID() {
		t.Fatalf("resumed id = %s", resumed.ID())
	}

	if err := client.DeleteSession(ctx, session.ID()); err != nil {
		t.Fatalf("delete: %v", err)
	}
	infos, err = client.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("infos after delete = %+v", infos)
	}
}

func TestSessionToolIsolation(t *testing.T) {
	client := newTestClient(t)
	toolA := DefineTool("shared", "from A", func(struct{}, ToolContext) (string, error) { return "a", nil })
	toolB := DefineTool("shared", "from B", func(struct{}, ToolContext) (string, error) { return "b", nil })

	first, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{toolA}})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	defer first.Close()
	second, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{toolB}})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	defer second.Close()

	if got := sharedToolDescription(client.rt.dispatcher.MaterializeFor(first.ID(), nil).Definitions); got != "from A" {
		t.Fatalf("first session tool = %q", got)
	}
	if got := sharedToolDescription(client.rt.dispatcher.MaterializeFor(second.ID(), nil).Definitions); got != "from B" {
		t.Fatalf("second session tool = %q", got)
	}
}

func sharedToolDescription(definitions []llmschema.ToolDefinition) string {
	for _, definition := range definitions {
		if definition.Name == "shared" {
			return definition.Description
		}
	}
	return ""
}

func TestDefineToolSchema(t *testing.T) {
	type params struct {
		City string `json:"city" jsonschema:"city name"`
		Unit string `json:"unit,omitempty"`
	}
	definition := SchemaForType(reflect.TypeOf((*params)(nil)).Elem())
	model := definition.Model()
	if model["additionalProperties"] != false {
		t.Fatalf("model = %#v", model)
	}
	required := model["required"].([]any)
	if len(required) != 1 || required[0] != "city" {
		t.Fatalf("required = %#v", required)
	}
}

func TestSessionSystemPromptIsolation(t *testing.T) {
	client := newTestClient(t)
	first, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent: "build", SystemPrompt: &SystemPromptConfig{Mode: SystemPromptReplace, Content: "S1"},
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer first.Close()
	second, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent: "build", SystemPrompt: &SystemPromptConfig{Mode: SystemPromptReplace, Content: "S2"},
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	defer second.Close()

	base := client.rt.agents.Get(schema.AgentID("build"))
	if base == nil || base.System == nil || *base.System != "You are a test." {
		t.Fatalf("base agent system = %v", valueOr(base.System))
	}
	firstAgent := client.rt.agents.Get(schema.AgentID(first.agentID))
	secondAgent := client.rt.agents.Get(schema.AgentID(second.agentID))
	if firstAgent == nil || firstAgent.System == nil || *firstAgent.System != "S1" {
		t.Fatalf("first agent = %+v", firstAgent)
	}
	if secondAgent == nil || secondAgent.System == nil || *secondAgent.System != "S2" {
		t.Fatalf("second agent = %+v", secondAgent)
	}
}

func TestClientPlugin(t *testing.T) {
	loaded := false
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:   "test-model",
		DataDir: t.TempDir(),
		LLM:     fakeLLM{reply: "x"},
		Plugins: []PluginConfig{{
			ID: "test",
			Effect: func(ctx *core.PluginContext) (func(), error) {
				loaded = true
				_, err := ctx.Agents.Transform(func(draft core.AgentDraft) error {
					draft.Update(schema.AgentID("build"), func(info *schema.AgentInfo) {
						system := "from plugin"
						info.System = &system
					})
					return nil
				})
				return func() {}, err
			},
		}},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if !loaded {
		t.Fatal("plugin was not loaded")
	}
	info := client.rt.agents.Get(schema.AgentID("build"))
	if info == nil || info.System == nil || *info.System != "from plugin" {
		t.Fatalf("agent = %+v", info)
	}
}

func TestClientReferences(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:   "test-model",
		DataDir: t.TempDir(),
		LLM:     fakeLLM{reply: "x"},
		References: map[string]core.ReferenceSource{
			"local": core.ReferenceLocalSource{Type: "local", Path: schema.AbsolutePath("/tmp/ref")},
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	infos := client.Reference().List()
	if len(infos) != 1 || infos[0].Name != "local" || infos[0].Path != schema.AbsolutePath("/tmp/ref") {
		t.Fatalf("references = %+v", infos)
	}
}

func TestClientLoadsConfigAgentsAndSkills(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "agent", "review.md"),
		"---\ndescription: reviewer\nmode: subagent\n---\nReview code.\n")
	writeConfigFile(t, filepath.Join(dir, "skills", "foo", "SKILL.md"),
		"---\nname: foo\ndescription: foo skill\n---\nFoo body.\n")
	writeConfigFile(t, filepath.Join(dir, "skills", "foo", "reference.md"),
		"supporting notes that are not a skill\n")

	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		ConfigDirs: []string{dir},
		DataDir:    t.TempDir(),
		LLM:        fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	for _, id := range []string{"build", "plan", "general", "explore", "compaction", "title", "summary"} {
		if client.rt.agents.Get(schema.AgentID(id)) == nil {
			t.Fatalf("default agent %q missing", id)
		}
	}

	review := client.rt.agents.Get(schema.AgentID("review"))
	if review == nil {
		t.Fatal("config agent review missing")
	}
	if review.Description == nil || *review.Description != "reviewer" {
		t.Fatalf("review = %+v", review)
	}
	if review.System == nil || *review.System != "Review code." {
		t.Fatalf("review system = %v", review.System)
	}

	skills, err := client.Skills().List()
	if err != nil {
		t.Fatalf("skills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "foo" {
		t.Fatalf("skills = %+v", skills)
	}
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type errorLLM struct{ err error }

func (f errorLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f errorLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, f.err
}

func (f errorLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	return nil, f.err
}

func TestSessionSendErrorBreakDoesNotPanic(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        errorLLM{err: errors.New("boom")},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gotError := false
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			gotError = true
			break
		}
	}
	if !gotError {
		t.Fatal("expected the turn to report an error")
	}
}

type questionLLM struct {
	calls *int
	reply string
}

func (f questionLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f questionLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f questionLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	*f.calls++
	var events []llmschema.LLMEvent
	if *f.calls == 1 {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewToolCall("call-q", "question", map[string]any{
				"questions": []any{map[string]any{
					"question": "Pick a color",
					"header":   "color",
					"options": []any{
						map[string]any{"label": "red", "description": "warm"},
						map[string]any{"label": "blue", "description": "cool"},
					},
				}},
			}),
			llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
			llmschema.NewFinish(llmschema.ReasonToolCalls),
		}
	} else {
		events = []llmschema.LLMEvent{
			llmschema.NewStepStart(0),
			llmschema.NewTextStart("t1"),
			llmschema.NewTextDelta("t1", f.reply),
			llmschema.NewTextEnd("t1"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
			llmschema.NewFinish(llmschema.ReasonStop),
		}
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func TestSessionQuestionHandler(t *testing.T) {
	calls := 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        questionLLM{calls: &calls, reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	var asked []schema.QuestionInfo
	session, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent: "build",
		OnQuestion: func(request QuestionRequest) ([]schema.QuestionAnswer, error) {
			asked = request.Questions
			return []schema.QuestionAnswer{{"blue"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var text string
	for event, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if delta, ok := event.(TextDelta); ok {
			text += delta.Delta
		}
	}
	if len(asked) != 1 || asked[0].Question != "Pick a color" {
		t.Fatalf("asked = %+v", asked)
	}
	if text != "done" {
		t.Fatalf("text = %q", text)
	}
}

func TestQuestionDefaultRejects(t *testing.T) {
	inner := core.NewQuestion(core.QuestionConfig{})
	question := newHandlerQuestion(inner, nil)
	_, err := question.Ask(context.Background(), core.QuestionAskInput{
		SessionID: "s1",
		Questions: []schema.QuestionInfo{{Question: "ok?"}},
	})
	if err == nil {
		t.Fatal("expected the default handler to reject")
	}
	var rejected *core.QuestionRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("err = %v", err)
	}
}

func ptr[T any](value T) *T { return &value }

func TestRegisterAgentModeAndHidden(t *testing.T) {
	client := newTestClient(t)

	// A new agent with no mode defaults to "all" (matching opencode), not "primary".
	if err := client.RegisterAgent("fresh", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register fresh: %v", err)
	}
	if got := client.rt.agents.Get(schema.AgentID("fresh")).Mode; got != schema.AgentModeAll {
		t.Fatalf("fresh mode = %q, want %q", got, schema.AgentModeAll)
	}

	// Explicit subagent is honored and survives a later partial update.
	if err := client.RegisterAgent("researcher", AgentConfig{System: "s", Mode: ptr(schema.AgentModeSubagent)}); err != nil {
		t.Fatalf("register researcher: %v", err)
	}
	if got := client.rt.agents.Get(schema.AgentID("researcher")).Mode; got != schema.AgentModeSubagent {
		t.Fatalf("researcher mode = %q", got)
	}
	if err := client.RegisterAgent("researcher", AgentConfig{Description: "updated"}); err != nil {
		t.Fatalf("update researcher: %v", err)
	}
	if got := client.rt.agents.Get(schema.AgentID("researcher")).Mode; got != schema.AgentModeSubagent {
		t.Fatalf("researcher mode after partial update = %q", got)
	}

	// Updating a built-in subagent without mode must not flip it to primary.
	if err := client.RegisterAgent("explore", AgentConfig{System: "custom"}); err != nil {
		t.Fatalf("update explore: %v", err)
	}
	if got := client.rt.agents.Get(schema.AgentID("explore")).Mode; got != schema.AgentModeSubagent {
		t.Fatalf("explore mode = %q, want subagent", got)
	}

	// Hidden is only written when provided.
	if err := client.RegisterAgent("secret", AgentConfig{System: "s", Hidden: ptr(true)}); err != nil {
		t.Fatalf("register secret: %v", err)
	}
	if !client.rt.agents.Get(schema.AgentID("secret")).Hidden {
		t.Fatal("secret should be hidden")
	}
	if err := client.RegisterAgent("secret", AgentConfig{Description: "updated"}); err != nil {
		t.Fatalf("update secret: %v", err)
	}
	if !client.rt.agents.Get(schema.AgentID("secret")).Hidden {
		t.Fatal("secret should stay hidden after partial update")
	}
	if err := client.RegisterAgent("secret", AgentConfig{Hidden: ptr(false)}); err != nil {
		t.Fatalf("unhide secret: %v", err)
	}
	if client.rt.agents.Get(schema.AgentID("secret")).Hidden {
		t.Fatal("secret should be visible after explicit false")
	}
}

type taskLLM struct{ parentCalls *int }

func (f taskLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f taskLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f taskLLM) Stream(_ context.Context, req *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	for _, part := range req.System {
		if strings.Contains(part.Text, "file search specialist") {
			return eventSeq(textEvents("child result")), nil
		}
	}
	*f.parentCalls++
	if *f.parentCalls == 1 {
		return eventSeq(toolCallEvents("task", map[string]any{
			"description":   "research",
			"prompt":        "find something",
			"subagent_type": "explore",
		})), nil
	}
	encoded, err := json.Marshal(req.Messages)
	if err != nil || !strings.Contains(string(encoded), "child result") {
		return eventSeq(textEvents("missing child result")), nil
	}
	return eventSeq(textEvents("parent done")), nil
}

func textEvents(text string) []llmschema.LLMEvent {
	return []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", text),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop),
		llmschema.NewFinish(llmschema.ReasonStop),
	}
}

func toolCallEvents(name string, input map[string]any) []llmschema.LLMEvent {
	return []llmschema.LLMEvent{
		llmschema.NewStepStart(0),
		llmschema.NewToolCall("call-1", name, input),
		llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
		llmschema.NewFinish(llmschema.ReasonToolCalls),
	}
}

func eventSeq(events []llmschema.LLMEvent) iter.Seq2[llmschema.LLMEvent, error] {
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

func TestTaskToolRunsSubagent(t *testing.T) {
	calls := 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        taskLLM{parentCalls: &calls},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register: %v", err)
	}

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var text string
	for event, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if delta, ok := event.(TextDelta); ok {
			text += delta.Delta
		}
	}
	if text != "parent done" {
		t.Fatalf("text = %q", text)
	}

	var childID, permission string
	if err := client.rt.db.DB.QueryRow(
		"SELECT id, permission FROM session WHERE parent_id = ?", session.ID(),
	).Scan(&childID, &permission); err != nil {
		t.Fatalf("child session not found: %v", err)
	}
	if !strings.Contains(permission, "todowrite") || !strings.Contains(permission, "task") {
		t.Fatalf("child permission = %q, want derived todowrite/task denies", permission)
	}
}

func TestSubagentTypesFilteredByTaskPermission(t *testing.T) {
	client := newTestClient(t)

	listed := func(permissions schema.PermissionRuleset) map[schema.AgentID]bool {
		result := map[schema.AgentID]bool{}
		for _, item := range SubagentTypes(client.rt.agents, permissions) {
			result[item.ID] = true
		}
		return result
	}

	if !listed(nil)[schema.AgentID("explore")] {
		t.Fatal("explore should be listed without a deny")
	}
	deny := schema.PermissionRuleset{{Action: "task", Resource: "explore", Effect: schema.PermissionEffectDeny}}
	if listed(deny)[schema.AgentID("explore")] {
		t.Fatal("explore should be filtered out when task is denied")
	}
}

func TestTaskDescriptionUsesSessionRules(t *testing.T) {
	client := newTestClient(t)
	rules := schema.PermissionRuleset{{Action: TaskName, Resource: "explore", Effect: schema.PermissionEffectDeny}}
	registry, err := client.rt.newSessionRegistry("build", rules, nil)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	for _, def := range registry.Materialize(nil).Definitions {
		if def.Name != TaskName {
			continue
		}
		if strings.Contains(def.Description, "explore") {
			t.Fatalf("session rule should omit explore from the task description:\n%s", def.Description)
		}
		return
	}
	t.Fatal("task tool not materialized")
}

func TestRegisterAgentMergesPermissions(t *testing.T) {
	client := newTestClient(t)
	before := client.rt.agents.Get(schema.AgentID("build"))
	if before == nil || len(before.Permissions) == 0 {
		t.Fatal("build should start with default permissions")
	}
	added := schema.PermissionRuleset{{Action: "bash", Resource: "*", Effect: schema.PermissionEffectAllow}}
	if err := client.RegisterAgent("build", AgentConfig{Permissions: added}); err != nil {
		t.Fatalf("register: %v", err)
	}
	after := client.rt.agents.Get(schema.AgentID("build"))
	if len(after.Permissions) != len(before.Permissions)+1 {
		t.Fatalf("permissions len = %d, want %d (merge, not replace)", len(after.Permissions), len(before.Permissions)+1)
	}
	if got := after.Permissions[len(after.Permissions)-1]; got.Action != "bash" {
		t.Fatalf("last rule = %+v, want bash appended", got)
	}
}

func TestAlwaysPermissionRemembersRule(t *testing.T) {
	var handlerCalls int
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:   "test-model",
		DataDir: t.TempDir(),
		Permission: func(PermissionRequest) (PermissionDecision, error) {
			handlerCalls++
			return PermissionDecision{Allow: true, Always: true}, nil
		},
		LLM: fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent:       "build",
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	input := core.PermissionAssertInput{
		SessionID: string(session.ID()),
		Action:    "echo",
		Resources: []string{"*"},
		Save:      []string{"*"},
	}
	for i := 0; i < 2; i++ {
		if err := client.rt.permission.Assert(input); err != nil {
			t.Fatalf("assert %d: %v", i, err)
		}
	}
	if handlerCalls != 1 {
		t.Fatalf("handler called %d times, want 1 (Always should remember the rule)", handlerCalls)
	}
}

func TestRegisterAgentPermissionsAccumulate(t *testing.T) {
	client := newTestClient(t)
	base := len(client.rt.agents.Get(schema.AgentID("build")).Permissions)
	added := schema.PermissionRuleset{{Action: "bash", Resource: "*", Effect: schema.PermissionEffectAllow}}
	for i := 0; i < 2; i++ {
		if err := client.RegisterAgent("build", AgentConfig{Permissions: added}); err != nil {
			t.Fatalf("register %d: %v", i, err)
		}
	}
	if got := len(client.rt.agents.Get(schema.AgentID("build")).Permissions); got != base+2 {
		t.Fatalf("permissions len = %d, want %d (merge appends, it does not dedupe)", got, base+2)
	}
}

func TestAlwaysPermissionPersistsAcrossClients(t *testing.T) {
	dir := t.TempDir()
	askEcho := schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}}

	var calls1 int
	client1, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"), Model: "test-model",
		DataDir: dir,
		Permission: func(PermissionRequest) (PermissionDecision, error) {
			calls1++
			return PermissionDecision{Allow: true, Always: true}, nil
		},
		LLM: fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client1: %v", err)
	}
	if err := client1.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session1, err := client1.CreateSession(context.Background(), &SessionConfig{Agent: "build", Permissions: askEcho})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := session1.ID()
	input := core.PermissionAssertInput{
		SessionID: string(id),
		Action:    "echo",
		Resources: []string{"*"},
		Save:      []string{"*"},
	}
	if err := client1.rt.permission.Assert(input); err != nil {
		t.Fatalf("assert1: %v", err)
	}
	_ = session1.Close()
	_ = client1.Close()
	if calls1 != 1 {
		t.Fatalf("handler1 calls = %d, want 1", calls1)
	}

	// A fresh client (process restart) must still remember the saved rule.
	var calls2 int
	client2, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"), Model: "test-model",
		DataDir: dir,
		Permission: func(PermissionRequest) (PermissionDecision, error) {
			calls2++
			return PermissionDecision{Allow: true}, nil
		},
		LLM: fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client2: %v", err)
	}
	defer client2.Close()
	if err := client2.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session2, err := client2.ResumeSession(context.Background(), string(id), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer session2.Close()
	if err := client2.rt.permission.Assert(input); err != nil {
		t.Fatalf("assert2: %v", err)
	}
	if calls2 != 0 {
		t.Fatalf("handler2 calls = %d, want 0 (the saved rule must persist)", calls2)
	}
}

type depthLLM struct {
	parentCalls *int
	childCalls  *int
}

func (f depthLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}

func (f depthLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

func (f depthLLM) Stream(_ context.Context, req *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	for _, part := range req.System {
		if strings.Contains(part.Text, "nested specialist") {
			*f.childCalls++
			if *f.childCalls == 1 {
				return eventSeq(toolCallEvents("task", map[string]any{
					"description": "nested", "prompt": "x", "subagent_type": "nested",
				})), nil
			}
			encoded, _ := json.Marshal(req.Messages)
			if !strings.Contains(string(encoded), "Subagent depth limit reached") {
				return eventSeq(textEvents("missing depth error")), nil
			}
			return eventSeq(textEvents("child done")), nil
		}
	}
	*f.parentCalls++
	if *f.parentCalls == 1 {
		return eventSeq(toolCallEvents("task", map[string]any{
			"description": "research", "prompt": "find", "subagent_type": "nested",
		})), nil
	}
	encoded, _ := json.Marshal(req.Messages)
	if !strings.Contains(string(encoded), "child done") {
		return eventSeq(textEvents("missing child")), nil
	}
	return eventSeq(textEvents("parent done")), nil
}

func TestTaskToolDepthLimit(t *testing.T) {
	parentCalls, childCalls := 0, 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        depthLLM{parentCalls: &parentCalls, childCalls: &childCalls},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	// A subagent that is allowed to call task, so the depth limit (not the
	// permission gate) is what blocks the nested call.
	mode := schema.AgentModeSubagent
	if err := client.RegisterAgent("nested", AgentConfig{
		System: "You are a nested specialist.",
		Mode:   &mode,
		Permissions: schema.PermissionRuleset{
			{Action: "*", Resource: "*", Effect: schema.PermissionEffectAllow},
			{Action: "task", Resource: "*", Effect: schema.PermissionEffectAllow},
		},
	}); err != nil {
		t.Fatalf("register nested: %v", err)
	}
	if err := client.RegisterAgent("build", AgentConfig{System: "You are a test."}); err != nil {
		t.Fatalf("register build: %v", err)
	}

	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var text string
	for event, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if delta, ok := event.(TextDelta); ok {
			text += delta.Delta
		}
	}
	if text != "parent done" {
		t.Fatalf("text = %q", text)
	}
}

func TestSubagentDepth(t *testing.T) {
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:         "test-model",
		SubagentDepth: 3,
		DataDir:       t.TempDir(),
		LLM:           fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if client.rt.subagentDepth != 3 {
		t.Fatalf("subagentDepth = %d, want 3", client.rt.subagentDepth)
	}
}

func TestClientOnTaskProgress(t *testing.T) {
	calls := 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        taskLLM{parentCalls: &calls},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	var mu sync.Mutex
	var progress *ToolProgress
	client.On(func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		e, ok := event.(ToolProgress)
		if !ok {
			return
		}
		if structured, ok := e.Structured.(map[string]any); ok && structured["sessionId"] != nil {
			copied := e
			progress = &copied
		}
	})

	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if progress == nil {
		t.Fatal("no task progress observed")
	}
	structured := progress.Structured.(map[string]any)
	if structured["subagentType"] != "explore" || structured["description"] != "research" {
		t.Fatalf("structured = %+v", structured)
	}
	if childID, _ := structured["sessionId"].(string); childID == "" {
		t.Fatalf("structured sessionId = %+v", structured["sessionId"])
	}
}

func TestTaskProgressPersisted(t *testing.T) {
	calls := 0
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:      "test-model",
		DataDir:    t.TempDir(),
		Permission: ApproveAll,
		LLM:        taskLLM{parentCalls: &calls},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	var persisted int
	if err := client.rt.db.DB.QueryRow(
		"SELECT COUNT(*) FROM `event` WHERE `aggregate_id` = ? AND `type` LIKE 'session.next.tool.progress%'", session.ID(),
	).Scan(&persisted); err != nil {
		t.Fatalf("query events: %v", err)
	}
	if persisted < 1 {
		t.Fatalf("persisted tool.progress events = %d, want >= 1", persisted)
	}
}

func TestMapEventCompactionMessageID(t *testing.T) {
	event, ok := mapEvent(schema.EventPayload{
		Type: schema.SessionEventCompactionStarted.Type,
		Data: map[string]any{"sessionID": "ses_1", "messageID": "msg_1", "reason": "auto"},
	})
	if !ok {
		t.Fatal("compaction.started not mapped")
	}
	started, ok := event.(CompactionStarted)
	if !ok {
		t.Fatalf("event = %T", event)
	}
	if started.MessageID != "msg_1" {
		t.Fatalf("MessageID = %q, want msg_1", started.MessageID)
	}
}

func TestSessionStatusEvents(t *testing.T) {
	client := newTestClient(t)
	session, err := client.CreateSession(context.Background(), &SessionConfig{Agent: "build"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	var mu sync.Mutex
	var statuses []string
	unsubscribe := client.On(func(event Event) {
		if status, ok := event.(SessionStatus); ok && status.SessionID == session.ID() {
			mu.Lock()
			statuses = append(statuses, status.Status)
			mu.Unlock()
		}
	})
	defer unsubscribe()

	for _, err := range session.Send(context.Background(), MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen := append([]string(nil), statuses...)
		mu.Unlock()
		if len(seen) >= 2 && seen[0] == "busy" && seen[len(seen)-1] == "idle" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("statuses = %v, want busy...idle", statuses)
}

func TestSessionPermissionsApply(t *testing.T) {
	var calls, permissionCalls int
	executed := false
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:   "test-model",
		DataDir: t.TempDir(),
		Permission: func(request PermissionRequest) (PermissionDecision, error) {
			permissionCalls++
			return PermissionDecision{Allow: false, Message: "denied"}, nil
		},
		LLM: toolCallLLM{calls: &calls, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	echo := DefineTool("echo", "echo", func(struct{}, ToolContext) (string, error) {
		executed = true
		return "ok", nil
	})
	session, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent: "build",
		Tools: []Tool{echo},
		// The agent allows everything; this per-session rule turns echo into ask.
		Permissions: schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sendErr error
	for _, err := range session.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			sendErr = err
			break // a denied tool halts the turn
		}
	}
	if permissionCalls == 0 {
		t.Fatal("session permission (ask) was not applied: permission handler not called")
	}
	if executed {
		t.Fatal("tool executed despite the deny decision")
	}
	var declined *core.PermissionDeclinedError
	if !errors.As(sendErr, &declined) {
		t.Fatalf("send error = %v, want PermissionDeclinedError", sendErr)
	}
}

func TestResumeSessionKeepsPermissions(t *testing.T) {
	dir := t.TempDir()
	askEcho := schema.PermissionRuleset{{Action: "echo", Resource: "*", Effect: schema.PermissionEffectAsk}}

	var calls1 int
	client1, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"), Model: "test-model",
		DataDir: dir, Permission: ApproveAll,
		LLM: toolCallLLM{calls: &calls1, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client1: %v", err)
	}
	if err := client1.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	echo := DefineTool("echo", "echo", func(struct{}, ToolContext) (string, error) { return "ok", nil })
	session1, err := client1.CreateSession(context.Background(), &SessionConfig{Agent: "build", Tools: []Tool{echo}, Permissions: askEcho})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := session1.ID()
	_ = session1.Close()
	_ = client1.Close()

	// A second client simulates a process restart: the session ruleset is read
	// from the stored session row, so the ask rule survives.
	var calls2, permissionCalls int
	client2, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"), Model: "test-model",
		DataDir: dir,
		Permission: func(PermissionRequest) (PermissionDecision, error) {
			permissionCalls++
			return PermissionDecision{Allow: true}, nil
		},
		LLM: toolCallLLM{calls: &calls2, toolName: "echo", reply: "done"},
	})
	if err != nil {
		t.Fatalf("new client2: %v", err)
	}
	defer client2.Close()
	if err := client2.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session2, err := client2.ResumeSession(context.Background(), id, &SessionConfig{Agent: "build", Tools: []Tool{echo}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer session2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, err := range session2.Send(ctx, MessageOptions{Prompt: "hi"}) {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if permissionCalls == 0 {
		t.Fatal("resumed session lost its ask rule: permission handler not called")
	}
}

// readCallLLM calls the read tool once, then replies.
type readCallLLM struct {
	path  string
	calls int
}

func (f *readCallLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return nil, nil
}
func (f *readCallLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}
func (f *readCallLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	f.calls++
	if f.calls == 1 {
		return eventSeq(toolCallEvents("read", map[string]any{"path": f.path})), nil
	}
	return eventSeq(textEvents("done")), nil
}

func TestRuleDenyDoesNotHaltTurn(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("SECRET=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:            "test-model",
		WorkingDirectory: dir,
		DataDir:          t.TempDir(),
		Permission:       ApproveAll,
		LLM:              &readCallLLM{path: envFile},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	session, err := client.CreateSession(context.Background(), &SessionConfig{
		Agent: "build",
		Permissions: schema.PermissionRuleset{
			{Action: "read", Resource: "*.env", Effect: schema.PermissionEffectDeny},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var text string
	for event, err := range session.Send(ctx, MessageOptions{Prompt: "read it"}) {
		if err != nil {
			t.Fatalf("a rule deny must not halt the turn: %v", err)
		}
		if delta, ok := event.(TextDelta); ok {
			text += delta.Delta
		}
	}
	if text != "done" {
		t.Fatalf("text = %q, want done", text)
	}
}

func TestNewClientRequiresProtocolOrModelInfo(t *testing.T) {
	_, err := NewClient(&ClientOptions{
		Model:   "test-model",
		DataDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "requires ModelInfo or Protocol") {
		t.Fatalf("err = %v, want a missing-protocol error", err)
	}
}

func TestNewClientRejectsUnknownProtocol(t *testing.T) {
	_, err := NewClient(&ClientOptions{
		Model:    "test-model",
		Protocol: "bogus",
		DataDir:  t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "unknown protocol") {
		t.Fatalf("err = %v, want an unknown-protocol error", err)
	}
}

func TestCompactionSettings(t *testing.T) {
	defaults := compactionSettings(&ClientOptions{})
	if !defaults.Auto || defaults.Buffer == 0 || defaults.Tokens == 0 {
		t.Fatalf("defaults = %+v", defaults)
	}

	off := false
	configured := compactionSettings(&ClientOptions{Compaction: &CompactionConfig{
		Auto:                 &off,
		PreserveRecentTokens: 1234,
		Reserved:             4321,
	}})
	if configured.Auto || configured.Tokens != 1234 || configured.Buffer != 4321 {
		t.Fatalf("configured = %+v", configured)
	}

	partial := compactionSettings(&ClientOptions{Compaction: &CompactionConfig{PreserveRecentTokens: 999}})
	if !partial.Auto || partial.Tokens != 999 || partial.Buffer != defaults.Buffer {
		t.Fatalf("partial = %+v", partial)
	}
}

func TestToolOutputSettings(t *testing.T) {
	if got := toolOutputSettings(&ClientOptions{}); got.MaxLines != 0 || got.MaxBytes != 0 {
		t.Fatalf("defaults = %+v", got)
	}
	got := toolOutputSettings(&ClientOptions{ToolOutput: &ToolOutputConfig{MaxLines: 10, MaxBytes: 20}})
	if got.MaxLines != 10 || got.MaxBytes != 20 {
		t.Fatalf("got = %+v", got)
	}
}

func TestClientCleanupToolOutput(t *testing.T) {
	store := core.NewToolOutputStore(core.Global{Data: t.TempDir()}, core.ToolOutputSettings{})
	client := &Client{rt: &runtime{toolOutput: store}}
	if err := client.CleanupToolOutput(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if err := (&Client{}).CleanupToolOutput(); err != nil {
		t.Fatalf("cleanup without a runtime: %v", err)
	}
}

func TestRuntimeToolOutputCleanupStopsOnCancel(t *testing.T) {
	store := core.NewToolOutputStore(core.Global{Data: t.TempDir()}, core.ToolOutputSettings{})
	rt := &runtime{toolOutput: store}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		rt.runToolOutputCleanup(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup loop did not stop on cancel")
	}
}

func TestClientDataRootControlsToolOutput(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(&ClientOptions{
		Protocol: "openai-responses", Credentials: APIKey("k"),
		Model:    "test-model",
		DataDir:  t.TempDir(),
		DataRoot: root,
		LLM:      fakeLLM{reply: "x"},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	long := strings.Repeat("line\n", core.ToolOutputStoreMaxLines+100)
	result, err := client.rt.toolOutput.Bound(core.ToolOutputStoreBoundInput{
		SessionID:  "ses_1",
		ToolCallID: "call_1",
		Output: llmschema.ToolOutput{Content: []llmschema.ToolContent{
			llmschema.ToolTextContent{Type: "text", Text: long},
		}},
	})
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if len(result.OutputPaths) != 1 {
		t.Fatalf("paths = %v", result.OutputPaths)
	}
	want := filepath.Join(root, "tool-output") + string(filepath.Separator)
	if !strings.HasPrefix(result.OutputPaths[0], want) {
		t.Fatalf("output path = %q, want it under %q", result.OutputPaths[0], want)
	}
}
