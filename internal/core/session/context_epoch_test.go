package session

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/core/systemcontext"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

type contextEpochTestPublisher struct {
	db        *database.Database
	published int
}

func (p *contextEpochTestPublisher) PublishWithCommit(
	def schema.EventDefinition,
	data any,
	commit func(tx DB, seq int) error,
) (*schema.EventPayloadDurable, error) {
	if def.Type != schema.SessionEventContextUpdated.Type {
		return nil, errors.New("unexpected event " + def.Type)
	}
	p.published++
	if err := commit(p.db.DB, p.published); err != nil {
		return nil, err
	}
	return &schema.EventPayloadDurable{Seq: p.published}, nil
}

func epochContext(key systemcontext.Key, value *string, available *bool) systemcontext.SystemContext {
	return systemcontext.Make(systemcontext.Source[string]{
		Key:      key,
		Load:     func() (string, bool, error) { return *value, *available, nil },
		Baseline: func(current string) string { return "base:" + current },
		Update:   func(previous, current string) string { return "update:" + previous + "->" + current },
		Removed:  func(previous string) string { return "removed:" + previous },
	})
}

func storedEpochSnapshot(t *testing.T, db *database.Database, sessionID schema.SessionID) systemcontext.Snapshot {
	t.Helper()
	row, err := contextEpochFind(db.DB, sessionID)
	if err != nil || row == nil {
		t.Fatalf("find epoch: %v %+v", err, row)
	}
	var snapshot systemcontext.Snapshot
	if err := json.Unmarshal([]byte(row.Snapshot), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return snapshot
}

func TestContextEpochInitializeAndPrepare(t *testing.T) {
	db := openSessionTestDB(t)
	sessionID := schema.SessionID("ses_1")
	value := "a"
	available := true
	context := epochContext("sys/one", &value, &available)

	initialized, err := ContextEpochInitialize(db.DB, context, sessionID)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initialized == nil || initialized.Baseline != "base:a" || initialized.BaselineSeq != -1 {
		t.Fatalf("initialized = %+v", initialized)
	}

	again, err := ContextEpochInitialize(db.DB, context, sessionID)
	if err != nil {
		t.Fatalf("initialize again: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no-op, got %+v", again)
	}

	publisher := &contextEpochTestPublisher{db: db}
	prepared, err := ContextEpochPrepare(db.DB, publisher, context, sessionID)
	if err != nil {
		t.Fatalf("prepare unchanged: %v", err)
	}
	if prepared.Baseline != "base:a" || prepared.BaselineSeq != -1 || publisher.published != 0 {
		t.Fatalf("unchanged = %+v published=%d", prepared, publisher.published)
	}

	value = "b"
	prepared, err = ContextEpochPrepare(db.DB, publisher, context, sessionID)
	if err != nil {
		t.Fatalf("prepare updated: %v", err)
	}
	if prepared.Baseline != "base:a" || prepared.BaselineSeq != -1 || publisher.published != 1 {
		t.Fatalf("updated = %+v published=%d", prepared, publisher.published)
	}
	snapshot := storedEpochSnapshot(t, db, sessionID)
	if string(snapshot["sys/one"].Value) != `"b"` {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	prepared, err = ContextEpochPrepare(db.DB, publisher, context, sessionID)
	if err != nil {
		t.Fatalf("prepare after advance: %v", err)
	}
	if publisher.published != 1 {
		t.Fatalf("expected no new publish, got %d", publisher.published)
	}
}

func TestContextEpochPrepareReplacement(t *testing.T) {
	db := openSessionTestDB(t)
	sessionID := schema.SessionID("ses_1")
	if _, err := db.DB.Exec(
		"INSERT INTO `event_sequence` (`aggregate_id`,`seq`) VALUES (?, ?)", string(sessionID), 5,
	); err != nil {
		t.Fatalf("insert sequence: %v", err)
	}
	value := "a"
	available := true
	context := epochContext("sys/one", &value, &available)

	initialized, err := ContextEpochInitialize(db.DB, context, sessionID)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initialized.BaselineSeq != 5 {
		t.Fatalf("baseline seq = %d", initialized.BaselineSeq)
	}

	insertMessage(t, db, "msg_c", "ses_1", "compaction", 6, `{"reason":"auto","summary":"s","recent":"r","time":{"created":0}}`)

	value = "z"
	publisher := &contextEpochTestPublisher{db: db}
	prepared, err := ContextEpochPrepare(db.DB, publisher, context, sessionID)
	if err != nil {
		t.Fatalf("prepare replacement: %v", err)
	}
	if prepared.Baseline != "base:z" || prepared.BaselineSeq != 6 {
		t.Fatalf("replacement = %+v", prepared)
	}
	if publisher.published != 0 {
		t.Fatalf("expected no publish, got %d", publisher.published)
	}
}

func TestContextEpochPrepareDecodeError(t *testing.T) {
	db := openSessionTestDB(t)
	sessionID := schema.SessionID("ses_1")
	if _, err := db.DB.Exec(
		"INSERT INTO `session_context_epoch` (`session_id`,`baseline`,`snapshot`,`baseline_seq`) VALUES (?,?,?,?)",
		string(sessionID), "base", `{"invalid":{"value":"bad"}}`, 0,
	); err != nil {
		t.Fatalf("insert epoch: %v", err)
	}
	value := "a"
	available := true
	context := epochContext("sys/one", &value, &available)

	_, err := ContextEpochPrepare(db.DB, &contextEpochTestPublisher{db: db}, context, sessionID)
	var decodeErr *ContextSnapshotDecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected ContextSnapshotDecodeError, got %v", err)
	}
}

func TestContextEpochReset(t *testing.T) {
	db := openSessionTestDB(t)
	sessionID := schema.SessionID("ses_1")
	value := "a"
	available := true
	context := epochContext("sys/one", &value, &available)

	if _, err := ContextEpochInitialize(db.DB, context, sessionID); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := ContextEpochReset(db.DB, sessionID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	found, err := contextEpochFind(db.DB, sessionID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found != nil {
		t.Fatalf("expected epoch removed, got %+v", found)
	}
}
