package wire

import (
	core "github.com/jiaweisky/go-agent-sdk/pkg/core"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"

	"github.com/jiaweisky/go-agent-sdk/internal/core/session"
)

// NewSessionRevertSnapshot wires the core Snapshot into session.RevertSnapshot.
func NewSessionRevertSnapshot(snapshot *core.Snapshot) session.RevertSnapshot {
	return sessionRevertSnapshot{snapshot: snapshot}
}

type sessionRevertSnapshot struct{ snapshot *core.Snapshot }

func (s sessionRevertSnapshot) Capture() (*string, error) {
	id, err := s.snapshot.Capture()
	if err != nil || id == nil {
		return nil, err
	}
	value := string(*id)
	return &value, nil
}

func (s sessionRevertSnapshot) Restore(files map[schema.RelativePath]string) error {
	converted := make(map[schema.RelativePath]core.SnapshotID, len(files))
	for file, id := range files {
		converted[file] = core.SnapshotID(id)
	}
	return s.snapshot.Restore(core.SnapshotRestoreInput{Files: converted})
}

func (s sessionRevertSnapshot) Diff(from, to string, paths []schema.RelativePath) ([]schema.RevertFileDiff, error) {
	diff, err := s.snapshot.Diff(core.SnapshotDiffInput{
		SnapshotCompareInput: core.SnapshotCompareInput{From: core.SnapshotID(from), To: core.SnapshotID(to)},
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
