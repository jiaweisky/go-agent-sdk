package session

import (
	"errors"
	"testing"

	"github.com/user/go-agent-sdk/schema"
)

type fakeRevertSnapshot struct {
	restored []map[schema.RelativePath]string
	diff     []schema.RevertFileDiff
}

func (f *fakeRevertSnapshot) Capture() (*string, error) {
	id := "tree"
	return &id, nil
}

func (f *fakeRevertSnapshot) Restore(files map[schema.RelativePath]string) error {
	f.restored = append(f.restored, files)
	return nil
}

func (f *fakeRevertSnapshot) Diff(from, to string, paths []schema.RelativePath) ([]schema.RevertFileDiff, error) {
	return f.diff, nil
}

func revertStrPtr(value string) *string { return &value }

func TestRevertPlan(t *testing.T) {
	db := openSessionTestDB(t)
	insertMessage(t, db, "msg_boundary", "ses_1", "assistant", 1,
		`{"agent":"build","model":{"id":"m","providerID":"p"},"content":[],"time":{"created":0}}`)
	insertMessage(t, db, "msg_later", "ses_1", "assistant", 2,
		`{"agent":"build","model":{"id":"m","providerID":"p"},"content":[],"snapshot":{"start":"tree1","files":["a.txt","b.txt"]},"time":{"created":0}}`)

	files, err := RevertPlan(db.DB, "ses_1", "msg_boundary")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(files) != 2 || files["a.txt"] != "tree1" || files["b.txt"] != "tree1" {
		t.Fatalf("files = %v", files)
	}

	_, err = RevertPlan(db.DB, "ses_1", "msg_missing")
	var notFound *RevertMessageNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestRevertStage(t *testing.T) {
	db := openSessionTestDB(t)
	insertMessage(t, db, "msg_boundary", "ses_1", "assistant", 1,
		`{"agent":"build","model":{"id":"m","providerID":"p"},"content":[],"time":{"created":0}}`)
	insertMessage(t, db, "msg_later", "ses_1", "assistant", 2,
		`{"agent":"build","model":{"id":"m","providerID":"p"},"content":[],"snapshot":{"start":"tree1","files":["a.txt"]},"time":{"created":0}}`)

	snapshot := &fakeRevertSnapshot{diff: []schema.RevertFileDiff{{Path: "a.txt", Patch: "@@ patch @@"}}}
	publisher := &recordingPublisher{}
	revert, err := RevertStage(db.DB, publisher, snapshot, schema.SessionInfo{ID: "ses_1"}, "msg_boundary", nil)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if revert.Snapshot == nil || *revert.Snapshot != "tree" {
		t.Fatalf("snapshot = %v", revert.Snapshot)
	}
	if revert.Diff == nil || *revert.Diff != "@@ patch @@" {
		t.Fatalf("diff = %v", revert.Diff)
	}
	if len(revert.Files) != 1 || revert.Files[0].Path != "a.txt" {
		t.Fatalf("files = %+v", revert.Files)
	}
	if len(publisher.published) != 1 || publisher.published[0].Type != schema.SessionEventRevertStaged.Type {
		t.Fatalf("published = %+v", publisher.published)
	}
	if len(snapshot.restored) != 1 || snapshot.restored[0]["a.txt"] != "tree1" {
		t.Fatalf("restored = %+v", snapshot.restored)
	}
}

func TestRevertClearAndCommit(t *testing.T) {
	snapshot := &fakeRevertSnapshot{}
	publisher := &recordingPublisher{}
	session := schema.SessionInfo{
		ID: "ses_1",
		Revert: &schema.RevertState{
			MessageID: "msg_boundary",
			Snapshot:  revertStrPtr("tree"),
			Files:     []schema.RevertFileDiff{{Path: "a.txt"}},
		},
	}
	if err := RevertClear(publisher, snapshot, session); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(snapshot.restored) != 1 || snapshot.restored[0]["a.txt"] != "tree" {
		t.Fatalf("restored = %+v", snapshot.restored)
	}
	if err := RevertCommit(publisher, session); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(publisher.published) != 2 ||
		publisher.published[0].Type != schema.SessionEventRevertCleared.Type ||
		publisher.published[1].Type != schema.SessionEventRevertCommitted.Type {
		t.Fatalf("published = %+v", publisher.published)
	}
}
