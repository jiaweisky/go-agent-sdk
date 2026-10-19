package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
	rootschema "github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type stubTodoUpdater struct {
	sessionID rootschema.SessionID
	todos     []rootschema.SessionTodoInfo
	err       error
}

func (s *stubTodoUpdater) Update(sessionID rootschema.SessionID, todos []rootschema.SessionTodoInfo) error {
	s.sessionID = sessionID
	s.todos = todos
	return s.err
}

func settleTodoWrite(t *testing.T, deps TodoWriteDeps, input map[string]any) *Settlement {
	t.Helper()
	registry := NewRegistry()
	if _, err := RegisterTodoWrite(registry, deps); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := registry.Materialize(nil).Settle(context.Background(), ExecuteInput{
		SessionID:          "ses_todowrite_test",
		Agent:              "build",
		AssistantMessageID: "msg_1",
		Call: schema.ToolCallPart{
			ID:    "call-todowrite",
			Name:  TodoWriteName,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	return result
}

func TestTodoWriteUpdatesAndFormats(t *testing.T) {
	updater := &stubTodoUpdater{}
	permission := &bashTestPermission{}
	result := settleTodoWrite(t, TodoWriteDeps{Todos: updater, Permission: permission}, map[string]any{
		"todos": []any{
			map[string]any{"content": "first", "status": "pending", "priority": "high"},
			map[string]any{"content": "second", "status": "completed", "priority": "low"},
		},
	})

	expected := TodoWriteToModelOutput(TodoWriteOutput{Todos: []rootschema.SessionTodoInfo{
		{Content: "first", Status: "pending", Priority: "high"},
		{Content: "second", Status: "completed", Priority: "low"},
	}})
	if result.Result.Type != "text" || result.Result.Value != expected {
		t.Fatalf("result = %+v, expected %q", result.Result, expected)
	}
	if updater.sessionID != "ses_todowrite_test" || len(updater.todos) != 2 {
		t.Fatalf("updater = %+v", updater)
	}
	if len(permission.assertions) != 1 {
		t.Fatalf("assertions = %d", len(permission.assertions))
	}
	assertion := permission.assertions[0]
	if assertion.Action != TodoWriteName || len(assertion.Resources) != 1 || assertion.Resources[0] != "*" ||
		len(assertion.Save) != 1 || assertion.Save[0] != "*" {
		t.Fatalf("assertion = %+v", assertion)
	}
}

func TestTodoWriteMapsErrors(t *testing.T) {
	result := settleTodoWrite(t, TodoWriteDeps{
		Todos:      &stubTodoUpdater{err: errors.New("boom")},
		Permission: &bashTestPermission{},
	}, map[string]any{
		"todos": []any{map[string]any{"content": "x", "status": "pending", "priority": "high"}},
	})
	if result.Result.Type != "error" || result.Result.Value != "Unable to update todos" {
		t.Fatalf("result = %+v", result.Result)
	}

	denied := settleTodoWrite(t, TodoWriteDeps{
		Todos:      &stubTodoUpdater{},
		Permission: &bashTestPermission{denyAction: TodoWriteName},
	}, map[string]any{"todos": []any{}})
	got, _ := denied.Result.Value.(string)
	if denied.Result.Type != "error" || !strings.HasPrefix(got, "Unable to update todos: permission blocked") {
		t.Fatalf("denied = %+v", denied.Result)
	}
}

func TestTodoWriteToModelOutputEmpty(t *testing.T) {
	if got := TodoWriteToModelOutput(TodoWriteOutput{}); got != "[]" {
		t.Fatalf("empty output = %q", got)
	}
}
