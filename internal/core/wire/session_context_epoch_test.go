package wire

import (
	"encoding/json"
	"path/filepath"
	"testing"

	core "github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/internal/core/session"
	"github.com/jiaweisky/go-agent-sdk/pkg/core/systemcontext"
)

func eventTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "event.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSessionContextEpochPrepareThroughEventService wires the real event
// service to SessionContextEpoch.prepare. It guards the transaction handle
// contract: the commit hook writes the advanced snapshot on the event
// transaction, which is required because the database pool allows a single
// connection (an outer write would deadlock).
func TestSessionContextEpochPrepareThroughEventService(t *testing.T) {
	db := eventTestDB(t)
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`,`worktree`,`sandboxes`,`time_created`,`time_updated`) VALUES ('p1','/project','[]',0,0)",
	); err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`time_created`,`time_updated`) " +
			"VALUES ('ses_1','p1','s','/project','t','1',0,0)",
	); err != nil {
		t.Fatalf("session: %v", err)
	}
	events := core.NewEvent(core.EventConfig{DB: db.DB, Durable: schema.Durable(schema.SessionEventDurableDefinitions)})
	if err := core.RegisterSessionProjectors(events); err != nil {
		t.Fatalf("register: %v", err)
	}

	value := "a"
	available := true
	context := systemcontext.Make(systemcontext.Source[string]{
		Key:      "sys/one",
		Load:     func() (string, bool, error) { return value, available, nil },
		Baseline: func(current string) string { return "base:" + current },
		Update:   func(previous, current string) string { return "update:" + previous + "->" + current },
	})
	sessionID := schema.SessionID("ses_1")
	publisher := sessionEventPublisher{events: events}

	initialized, err := session.ContextEpochInitialize(db.DB, context, sessionID)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initialized == nil || initialized.Baseline != "base:a" || initialized.BaselineSeq != -1 {
		t.Fatalf("initialized = %+v", initialized)
	}

	value = "b"
	prepared, err := session.ContextEpochPrepare(db.DB, publisher, context, sessionID)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Baseline != "base:a" || prepared.BaselineSeq != -1 {
		t.Fatalf("prepared = %+v", prepared)
	}

	var snapshotJSON string
	if err := db.DB.QueryRow(
		"SELECT `snapshot` FROM `session_context_epoch` WHERE `session_id` = ?", string(sessionID),
	).Scan(&snapshotJSON); err != nil {
		t.Fatalf("epoch: %v", err)
	}
	var snapshot systemcontext.Snapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if string(snapshot["sys/one"].Value) != `"b"` {
		t.Fatalf("snapshot = %s", snapshotJSON)
	}

	var messageType, text string
	if err := db.DB.QueryRow(
		"SELECT `type`, json_extract(`data`, '$.text') FROM `session_message` WHERE `session_id` = ?", string(sessionID),
	).Scan(&messageType, &text); err != nil {
		t.Fatalf("session_message: %v", err)
	}
	if messageType != "system" || text != "update:a->b" {
		t.Fatalf("message = %q %q", messageType, text)
	}

	var seq int
	if err := db.DB.QueryRow(
		"SELECT `seq` FROM `event_sequence` WHERE `aggregate_id` = ?", string(sessionID),
	).Scan(&seq); err != nil {
		t.Fatalf("event_sequence: %v", err)
	}
	if seq != 0 {
		t.Fatalf("seq = %d", seq)
	}
}
