package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	"github.com/user/go-agent-sdk/core/database"
	coresession "github.com/user/go-agent-sdk/core/session"
)

func openTurnTestStore(t *testing.T) (*database.Database, *coresession.TurnStore) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/p','[]',0,0)",
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`revert`,`time_created`,`time_updated`) " +
			"VALUES ('ses_1','p1','s','/p','t','1','build',NULL,NULL,0,0)",
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return db, coresession.NewTurnStore(db)
}

// newTask builds a task as the executor would: it already carries the mirrored
// session and agent metadata.
func newTask(id a2a.TaskID, contextID, agentID string, state a2a.TaskState) *a2a.Task {
	return &a2a.Task{
		ID:        id,
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: state},
		Metadata:  map[string]any{metaKeySession: "ses_1", metaKeyAgent: agentID},
	}
}

func TestTurnTaskStoreVersionSurvivesRestart(t *testing.T) {
	_, turns := openTurnTestStore(t)
	taskID := a2a.NewTaskID()
	task := newTask(taskID, "ctx-1", "build", a2a.TaskStateSubmitted)

	if _, err := NewTurnTaskStore(turns).Create(context.Background(), task); err != nil {
		t.Fatalf("create: %v", err)
	}
	task.Status.State = a2a.TaskStateWorking
	if _, err := NewTurnTaskStore(turns).Update(context.Background(), &taskstore.UpdateRequest{
		Task: task, PrevVersion: 1,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// A fresh store over the same database is a restart: the version must
	// persist in turn metadata, not memory.
	restarted := NewTurnTaskStore(turns)
	stored, err := restarted.Get(context.Background(), taskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Version != 2 {
		t.Fatalf("version = %d, want 2", stored.Version)
	}

	// A stale update must be rejected after the restart.
	if _, err := restarted.Update(context.Background(), &taskstore.UpdateRequest{
		Task: task, PrevVersion: 1,
	}); !errors.Is(err, taskstore.ErrConcurrentModification) {
		t.Fatalf("stale update err = %v, want ErrConcurrentModification", err)
	}
}

func TestTurnTaskStoreSessionForContextScopedByAgent(t *testing.T) {
	_, turns := openTurnTestStore(t)
	store := NewTurnTaskStore(turns)
	task := newTask(a2a.NewTaskID(), "ctx-1", "build", a2a.TaskStateSubmitted)
	if _, err := store.Create(context.Background(), task); err != nil {
		t.Fatalf("create: %v", err)
	}

	concrete, ok := store.(*turnTaskStore)
	if !ok {
		t.Fatal("store is not a *turnTaskStore")
	}
	if sessionID, found := concrete.SessionForContext("build", "ctx-1"); !found || sessionID != "ses_1" {
		t.Fatalf("same agent lookup = %q, %v; want ses_1, true", sessionID, found)
	}
	if _, found := concrete.SessionForContext("other", "ctx-1"); found {
		t.Fatal("session leaked across agents")
	}
}

func TestTurnTaskStoreListPagination(t *testing.T) {
	_, turns := openTurnTestStore(t)
	store := NewTurnTaskStore(turns)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		task := newTask(a2a.NewTaskID(), "ctx", "build", a2a.TaskStateCompleted)
		task.History = []*a2a.Message{
			a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("h1")),
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("h2")),
		}
		task.Artifacts = []*a2a.Artifact{{ID: a2a.NewArtifactID()}}
		if _, err := store.Create(ctx, task); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	first, err := store.List(ctx, &a2a.ListTasksRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first.Tasks) != 2 || first.TotalSize != 3 || first.NextPageToken == "" || first.PageSize != 2 {
		t.Fatalf("first page = %+v", first)
	}
	if first.Tasks[0].Artifacts != nil {
		t.Fatal("artifacts should be trimmed by default")
	}

	second, err := store.List(ctx, &a2a.ListTasksRequest{PageSize: 2, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(second.Tasks) != 1 || second.NextPageToken != "" || second.PageSize != 1 {
		t.Fatalf("second page = %+v", second)
	}

	length := 1
	trimmed, err := store.List(ctx, &a2a.ListTasksRequest{HistoryLength: &length, IncludeArtifacts: true})
	if err != nil {
		t.Fatalf("list trimmed: %v", err)
	}
	if len(trimmed.Tasks[0].History) != 1 {
		t.Fatalf("history = %d, want 1", len(trimmed.Tasks[0].History))
	}
	if len(trimmed.Tasks[0].Artifacts) != 1 {
		t.Fatal("artifacts should be kept with IncludeArtifacts")
	}

	filtered, err := store.List(ctx, &a2a.ListTasksRequest{ContextID: "other"})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if filtered.TotalSize != 0 {
		t.Fatalf("filtered total = %d, want 0", filtered.TotalSize)
	}

	if _, err := store.List(ctx, &a2a.ListTasksRequest{PageSize: 101}); err == nil {
		t.Fatal("page size over 100 should be rejected")
	}
}
