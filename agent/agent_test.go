package agent

import (
	"context"
	"iter"
	"reflect"
	"testing"
	"time"

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

func newTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(&ClientOptions{
		Provider:   ProviderConfig{Type: "openai", APIKey: "test"},
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
		Provider: ProviderConfig{
			Type: "openai-compatible", BaseURL: "https://api.deepseek.com/v1",
			APIKey: "test", Provider: "deepseek",
		},
		Model:   "deepseek-chat",
		DataDir: t.TempDir(),
		LLM:     fakeLLM{reply: "x"},
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
		Provider: ProviderConfig{Type: "openai", APIKey: "k"},
		Model:    "test-model",
		DataDir:  t.TempDir(),
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
	if err := client.RegisterAgent("build", AgentConfig{System: "s"}); err != nil {
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
			t.Fatalf("send: %v", err)
		}
	}
	if !executed {
		t.Fatal("tool was not executed")
	}
	if permissionCalls == 0 {
		t.Fatal("permission handler was not called")
	}
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
		Provider: ProviderConfig{Type: "openai", APIKey: "k"},
		Model:    "test-model",
		DataDir:  t.TempDir(),
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

func TestModelResolverUsesSessionModel(t *testing.T) {
	resolve, err := buildModelResolver(&ClientOptions{
		Provider: ProviderConfig{Type: "openai-compatible", Provider: "deepseek"},
		Model:    "default-model",
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
