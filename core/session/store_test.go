package session

import (
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func openSessionTestDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.Exec(
		"INSERT INTO `project` (`id`, `worktree`, `sandboxes`, `time_created`, `time_updated`) VALUES ('p1', '/project', '[]', 0, 0)",
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.DB.Exec(
		"INSERT INTO `session` (`id`,`project_id`,`slug`,`directory`,`title`,`version`,`agent`,`model`,`revert`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?)",
		"ses_1", "p1", "slug", "/project", "Title", "1", "build",
		`{"id":"m","providerID":"p","variant":"v"}`,
		`{"messageID":"msg_1"}`,
		10, 20,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return db
}

func insertMessage(t *testing.T, db *database.Database, id, sessionID, messageType string, seq int64, data string) {
	t.Helper()
	if _, err := db.DB.Exec(
		"INSERT INTO `session_message` (`id`,`session_id`,`type`,`seq`,`time_created`,`time_updated`,`data`) VALUES (?,?,?,?,?,?,?)",
		id, sessionID, messageType, seq, 0, 0, data,
	); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestStoreGetAndLoad(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	sessionID := schema.SessionID("ses_1")

	insertMessage(t, db, "msg_sys", "ses_1", "system", 0, `{"text":"sys","time":{"created":0}}`)
	insertMessage(t, db, "msg_user", "ses_1", "user", 1, `{"text":"hi","time":{"created":0}}`)
	insertMessage(t, db, "msg_comp", "ses_1", "compaction", 2, `{"reason":"auto","summary":"s","recent":"r","time":{"created":0}}`)
	insertMessage(t, db, "msg_asst", "ses_1", "assistant", 3, `{"agent":"build","model":{"id":"m","providerID":"p"},"content":[{"type":"text","id":"t","text":"done"}],"time":{"created":0}}`)

	info, err := store.Get(sessionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	row, err := scanSessionRow(db, sessionID)
	if err != nil || row == nil {
		t.Fatalf("scan row: %v %+v", err, row)
	}
	if row.Slug != "slug" || row.Version != "1" || row.SummaryAdditions != nil || row.TimeArchived != nil {
		t.Fatalf("row = %+v", row)
	}
	if info == nil || info.Title != "Title" || info.Location.Directory != "/project" {
		t.Fatalf("info = %+v", info)
	}
	if info.Model == nil || info.Model.ID != "m" || info.Model.ProviderID != "p" || info.Model.Variant == nil || *info.Model.Variant != "v" {
		t.Fatalf("model = %+v", info.Model)
	}
	if info.Revert == nil || info.Revert.MessageID != "msg_1" {
		t.Fatalf("revert = %+v", info.Revert)
	}
	if info.Tokens.Input != 0 || info.Time.Created != 10 || info.Time.Updated != 20 {
		t.Fatalf("info = %+v", info)
	}

	context, err := store.Context(sessionID)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(context) != 2 {
		t.Fatalf("context length = %d", len(context))
	}
	if _, ok := context[0].(schema.SessionMessageCompaction); !ok {
		t.Fatalf("context[0] = %T", context[0])
	}
	assistant, ok := context[1].(schema.SessionMessageAssistant)
	if !ok || len(assistant.Content) != 1 {
		t.Fatalf("context[1] = %#v", context[1])
	}
	if _, ok := assistant.Content[0].(schema.SessionMessageAssistantText); !ok {
		t.Fatalf("assistant content = %#v", assistant.Content[0])
	}

	message, err := store.Message("msg_user")
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	if message == nil || message.SessionID != sessionID {
		t.Fatalf("message = %+v", message)
	}
	if user, ok := message.Message.(schema.SessionMessageUser); !ok || user.Text != "hi" {
		t.Fatalf("message payload = %#v", message.Message)
	}

	if missing, err := store.Message("msg_missing"); err != nil || missing != nil {
		t.Fatalf("missing message = %+v, %v", missing, err)
	}
}

func TestHistoryRunnerBaseline(t *testing.T) {
	db := openSessionTestDB(t)
	sessionID := schema.SessionID("ses_1")

	insertMessage(t, db, "msg_sys0", "ses_1", "system", 0, `{"text":"old","time":{"created":0}}`)
	insertMessage(t, db, "msg_user1", "ses_1", "user", 1, `{"text":"one","time":{"created":0}}`)
	insertMessage(t, db, "msg_sys2", "ses_1", "system", 2, `{"text":"new","time":{"created":0}}`)

	all, err := Load(db, sessionID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("load = %d messages", len(all))
	}

	entries, err := EntriesForRunner(db, sessionID, 1)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 2 || entries[0].Seq != 1 || entries[1].Seq != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if _, ok := entries[0].Message.(schema.SessionMessageUser); !ok {
		t.Fatalf("entries[0] = %T", entries[0].Message)
	}

	runner, err := LoadForRunner(db, sessionID, 1)
	if err != nil {
		t.Fatalf("loadForRunner: %v", err)
	}
	if len(runner) != 2 {
		t.Fatalf("runner = %d", len(runner))
	}
}

func TestFromRowNullJSON(t *testing.T) {
	model := "null"
	revert := "null"
	info := FromRow(SessionRow{ID: "ses_1", ProjectID: "p1", Directory: "/d", Model: &model, Revert: &revert})
	if info.Model != nil || info.Revert != nil {
		t.Fatalf("expected nil model/revert, got %+v %+v", info.Model, info.Revert)
	}
}

func TestDecodeMessageRowPreservesIntegerPrecision(t *testing.T) {
	db := openSessionTestDB(t)
	const created = int64(9007199254740993) // 2^53 + 1, not representable as float64
	insertMessage(t, db, "msg_user", "ses_1", "user", 0,
		`{"text":"hi","time":{"created":9007199254740993}}`)

	messages, err := Load(db, schema.SessionID("ses_1"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	user, ok := messages[0].(schema.SessionMessageUser)
	if !ok {
		t.Fatalf("message = %T", messages[0])
	}
	if int64(user.Time.Created) != created {
		t.Fatalf("created = %d, want %d", user.Time.Created, created)
	}
	if user.ID != "msg_user" || user.Type != "user" {
		t.Fatalf("meta = %q %q", user.ID, user.Type)
	}
}
