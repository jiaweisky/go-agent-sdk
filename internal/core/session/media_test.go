package session

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestPersistMessageFiles(t *testing.T) {
	db := openSessionTestDB(t)
	sessionID := schema.SessionID("ses_1")
	name := "a.png"
	user := schema.SessionMessageUser{
		ID: "msg_user", Type: "user", Text: "look",
		Files: []schema.PromptFileAttachment{{URI: "data:image/png;base64,aGVsbG8=", Mime: "image/png", Name: &name}},
	}
	data, err := v2MessageData(user)
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	insertMessage(t, db, "msg_user", "ses_1", "user", 1, data)

	// Resolve the attachment to a file reference and persist it.
	user.Files[0].MediaSource = "file"
	user.Files[0].FileID = "file-1"
	user.Files[0].URI = ""
	if err := PersistMessage(db, sessionID, user); err != nil {
		t.Fatalf("persist: %v", err)
	}

	messages, err := Load(db, sessionID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := messages[0].(schema.SessionMessageUser)
	if !ok || len(got.Files) != 1 {
		t.Fatalf("messages = %+v", messages)
	}
	if got.Files[0].FileID != "file-1" || got.Files[0].MediaSource != "file" || got.Files[0].URI != "" {
		t.Fatalf("files = %+v", got.Files[0])
	}
}
