package session

import (
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type recordingPublisher struct {
	published []schema.EventDefinition
	data      []any
}

func (p *recordingPublisher) Publish(def schema.EventDefinition, data any) (*schema.EventPayloadDurable, error) {
	p.published = append(p.published, def)
	p.data = append(p.data, data)
	return nil, nil
}

func openTodoTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`, `worktree`, `sandboxes`, `time_created`, `time_updated`) VALUES ('p1', '/w', '[]', 0, 0)",
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`, `project_id`, `slug`, `directory`, `title`, `version`, `time_created`, `time_updated`) " +
			"VALUES ('ses_1', 'p1', 's', '/w', 't', '1', 0, 0)",
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return db
}

func TestTodoUpdateAndGet(t *testing.T) {
	db := openTodoTestDB(t)
	publisher := &recordingPublisher{}
	todo := NewTodo(db, publisher)
	sessionID := schema.SessionID("ses_1")

	todos := []schema.SessionTodoInfo{
		{Content: "first", Status: "pending", Priority: "high"},
		{Content: "second", Status: "in_progress", Priority: "low"},
	}
	if err := todo.Update(sessionID, todos); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := todo.Get(sessionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 || got[0].Content != "first" || got[1].Content != "second" {
		t.Fatalf("todos = %+v", got)
	}
	if len(publisher.published) != 1 || publisher.published[0].Type != "todo.updated" {
		t.Fatalf("published = %+v", publisher.published)
	}

	if err := todo.Update(sessionID, []schema.SessionTodoInfo{{Content: "only", Status: "completed", Priority: "medium"}}); err != nil {
		t.Fatalf("update replace: %v", err)
	}
	got, _ = todo.Get(sessionID)
	if len(got) != 1 || got[0].Content != "only" {
		t.Fatalf("todos after replace = %+v", got)
	}

	if err := todo.Update(sessionID, nil); err != nil {
		t.Fatalf("update clear: %v", err)
	}
	got, _ = todo.Get(sessionID)
	if len(got) != 0 {
		t.Fatalf("todos after clear = %+v", got)
	}
}
