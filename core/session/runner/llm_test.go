package runner

import (
	"context"
	"encoding/json"
	"iter"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/go-agent-sdk/core"
	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/core/reference"
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/core/skill"
	"github.com/user/go-agent-sdk/core/systemcontext"
	"github.com/user/go-agent-sdk/core/tool"
	"github.com/user/go-agent-sdk/jsonschema"
	"github.com/user/go-agent-sdk/llm/protocols"
	"github.com/user/go-agent-sdk/llm/route"
	llmschema "github.com/user/go-agent-sdk/llm/schema"
	"github.com/user/go-agent-sdk/schema"
)

type runnerTestEvents struct{ events core.Event }

func (e runnerTestEvents) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	payload, err := e.events.Publish(def, data, nil)
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}

func (e runnerTestEvents) PublishWithCommit(def schema.EventDefinition, data any, commit func(tx session.DB, seq int) error) (*schema.EventPayloadDurable, error) {
	payload, err := e.events.Publish(def, data, &core.EventPublishOptions{
		Commit: func(tx core.EventDB, seq int) error { return commit(tx, seq) },
	})
	if err != nil {
		return nil, err
	}
	return payload.Durable, nil
}

type runnerTestLLM struct {
	scripts [][]llmschema.LLMEvent
	calls   int
}

func (f *runnerTestLLM) Prepare(context.Context, *llmschema.LLMRequest) (*llmschema.PreparedRequest, error) {
	return &llmschema.PreparedRequest{}, nil
}

func (f *runnerTestLLM) Stream(context.Context, *llmschema.LLMRequest) (iter.Seq2[llmschema.LLMEvent, error], error) {
	index := f.calls
	f.calls++
	var events []llmschema.LLMEvent
	if index < len(f.scripts) {
		events = f.scripts[index]
	}
	return func(yield func(llmschema.LLMEvent, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}, nil
}

func (f *runnerTestLLM) Generate(context.Context, *llmschema.LLMRequest) (*llmschema.LLMResponse, error) {
	return nil, nil
}

type runnerTestAgents struct{}

func (runnerTestAgents) Select(*string) core.AgentSelection {
	return core.AgentSelection{ID: "build"}
}

type runnerTestSnapshots struct{}

func (runnerTestSnapshots) Capture() (*core.SnapshotID, error) { return nil, nil }
func (runnerTestSnapshots) Files(core.SnapshotCompareInput) ([]schema.RelativePath, error) {
	return nil, nil
}

type runnerTestConfig struct{}

func (runnerTestConfig) Entries() []core.ConfigEntry { return nil }

type runnerTestReferences struct{}

func (runnerTestReferences) List() ([]reference.Info, error) { return nil, nil }

func openRunnerTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`revert`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?)",
		"ses_1", "p1", "slug", "/project", "Title", "1", "build",
		`{"id":"test-model","providerID":"test"}`, `null`, 10, 20,
	); err != nil {
		t.Fatalf("session: %v", err)
	}
	return db
}

func newRunnerTestRunner(t *testing.T, db *database.Database, llm route.LLMClient) *LLMRunner {
	t.Helper()
	events := core.NewEvent(core.EventConfig{DB: db.DB, Durable: schema.Durable(schema.SessionEventDurableDefinitions)})
	if err := core.RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}
	configured := protocols.OpenAIChatHTTPRoute.With(route.RoutePatch{
		Provider: "test",
		Defaults: &route.RouteDefaultsInput{Limits: &llmschema.ModelLimits{Context: 100_000, Output: 1_000}},
	})
	resolved := configured.Model(route.RouteModelInput{ID: "test-model"})
	models := func(schema.SessionInfo) (route.Model, error) { return resolved, nil }

	registry := tool.NewRegistry()
	if _, err := registry.Register(map[string]tool.Tool{
		"echo": tool.Make(tool.Config[map[string]any, string, string]{
			Description: "echo",
			Input:       jsonschema.Raw(map[string]any{"type": "object"}),
			Output:      jsonschema.Raw(map[string]any{"type": "string"}),
			Execute: func(context.Context, map[string]any, tool.Context) (string, error) {
				return "ok", nil
			},
		}),
	}); err != nil {
		t.Fatalf("register echo: %v", err)
	}

	return NewLLMRunner(LLMRunner{
		Events:            runnerTestEvents{events: events},
		LLM:               llm,
		Agents:            runnerTestAgents{},
		Tools:             registry,
		Models:            models,
		Store:             session.NewStore(db),
		Location:          core.Location{LocationInfo: schema.LocationInfo{Directory: "/project"}},
		SystemContext:     systemcontext.NewRegistry(),
		SkillGuidance:     skill.NewGuidance(nil, nil),
		ReferenceGuidance: reference.NewGuidance(runnerTestReferences{}),
		Config:            runnerTestConfig{},
		Snapshots:         runnerTestSnapshots{},
		DB:                db,
	})
}

func runnerAssistantData(t *testing.T, db *database.Database) string {
	t.Helper()
	rows, err := db.DB.Query("SELECT `type`,`data` FROM `session_message` WHERE `session_id` = 'ses_1' ORDER BY `seq` ASC")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var combined strings.Builder
	for rows.Next() {
		var messageType, data string
		if err := rows.Scan(&messageType, &data); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if messageType == "assistant" {
			combined.WriteString(data)
		}
	}
	if combined.Len() == 0 {
		t.Fatalf("no assistant message")
	}
	return combined.String()
}

func TestLLMRunnerTextTurn(t *testing.T) {
	db := openRunnerTestDB(t)
	llm := &runnerTestLLM{scripts: [][]llmschema.LLMEvent{{
		llmschema.NewTextStart("t1"),
		llmschema.NewTextDelta("t1", "hello"),
		llmschema.NewTextEnd("t1"),
		llmschema.NewStepFinish(0, llmschema.ReasonStop),
	}}}
	runner := newRunnerTestRunner(t, db, llm)

	if err := runner.Run(context.Background(), RunInput{SessionID: "ses_1", Force: true}); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := runnerAssistantData(t, db)
	if !strings.Contains(data, "hello") {
		t.Fatalf("assistant data = %s", data)
	}
	if !strings.Contains(data, `"finish":"stop"`) {
		t.Fatalf("assistant finish = %s", data)
	}

	var seq int
	if err := db.DB.QueryRow("SELECT `seq` FROM `event_sequence` WHERE `aggregate_id` = 'ses_1'").Scan(&seq); err != nil {
		t.Fatalf("event_sequence: %v", err)
	}
	if seq < 0 {
		t.Fatalf("seq = %d", seq)
	}
}

func TestLLMRunnerToolCallContinuation(t *testing.T) {
	db := openRunnerTestDB(t)
	llm := &runnerTestLLM{scripts: [][]llmschema.LLMEvent{
		{
			llmschema.NewToolCall("call-1", "echo", map[string]any{"x": 1}),
			llmschema.NewStepFinish(0, llmschema.ReasonToolCalls),
		},
		{
			llmschema.NewTextStart("t2"),
			llmschema.NewTextDelta("t2", "done"),
			llmschema.NewTextEnd("t2"),
			llmschema.NewStepFinish(0, llmschema.ReasonStop),
		},
	}}
	runner := newRunnerTestRunner(t, db, llm)

	if err := runner.Run(context.Background(), RunInput{SessionID: "ses_1", Force: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if llm.calls != 2 {
		t.Fatalf("llm calls = %d, want 2 (tool settlement must continue)", llm.calls)
	}
	data := runnerAssistantData(t, db)
	if !strings.Contains(data, "ok") {
		t.Fatalf("assistant data missing tool result: %s", data)
	}
	if !strings.Contains(data, "done") {
		t.Fatalf("assistant data missing continuation text: %s", data)
	}
}

func TestLLMRunnerProviderErrorFailsStep(t *testing.T) {
	db := openRunnerTestDB(t)
	llm := &runnerTestLLM{scripts: [][]llmschema.LLMEvent{{
		llmschema.NewProviderErrorEvent("provider exploded"),
	}}}
	runner := newRunnerTestRunner(t, db, llm)

	if err := runner.Run(context.Background(), RunInput{SessionID: "ses_1", Force: true}); err != nil {
		t.Fatalf("run: %v", err)
	}

	rows, err := db.DB.Query("SELECT `data` FROM `session_message` WHERE `session_id` = 'ses_1' AND `type` = 'assistant'")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(data), &decoded) != nil {
			continue
		}
		if strings.Contains(data, "provider exploded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected failed assistant message mentioning the provider error")
	}
}
