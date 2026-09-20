package core

import (
	"github.com/user/go-agent-sdk/core/session"
	"github.com/user/go-agent-sdk/schema"
)

// NewSessionRevertSnapshot wires the core Snapshot into session.RevertSnapshot.
func NewSessionRevertSnapshot(snapshot *Snapshot) session.RevertSnapshot {
	return sessionRevertSnapshot{snapshot: snapshot}
}

type sessionRevertSnapshot struct{ snapshot *Snapshot }

func (s sessionRevertSnapshot) Capture() (*string, error) {
	id, err := s.snapshot.Capture()
	if err != nil || id == nil {
		return nil, err
	}
	value := string(*id)
	return &value, nil
}

func (s sessionRevertSnapshot) Restore(files map[schema.RelativePath]string) error {
	converted := make(map[schema.RelativePath]SnapshotID, len(files))
	for file, id := range files {
		converted[file] = SnapshotID(id)
	}
	return s.snapshot.Restore(SnapshotRestoreInput{Files: converted})
}

func (s sessionRevertSnapshot) Diff(from, to string, paths []schema.RelativePath) ([]schema.RevertFileDiff, error) {
	diff, err := s.snapshot.Diff(SnapshotDiffInput{
		SnapshotCompareInput: SnapshotCompareInput{From: SnapshotID(from), To: SnapshotID(to)},
		Paths:                paths,
	})
	if err != nil {
		return nil, err
	}
	result := make([]schema.RevertFileDiff, len(diff))
	for i, file := range diff {
		result[i] = schema.RevertFileDiff{
			Path:      file.Path,
			Status:    string(file.Status),
			Additions: file.Additions,
			Deletions: file.Deletions,
			Patch:     file.Patch,
		}
	}
	return result, nil
}
