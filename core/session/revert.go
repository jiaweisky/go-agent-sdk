package session

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// RevertMessageNotFoundError reports a revert target message that does not exist.
type RevertMessageNotFoundError struct {
	SessionID schema.SessionID
	MessageID schema.MessageID
}

func (e *RevertMessageNotFoundError) Error() string {
	return "revert boundary message not found: " + string(e.MessageID)
}

// RevertSnapshot is the narrow Snapshot surface SessionRevert needs.
type RevertSnapshot interface {
	Capture() (*string, error)
	Restore(files map[schema.RelativePath]string) error
	Diff(from, to string, paths []schema.RelativePath) ([]schema.RevertFileDiff, error)
}

// RevertPlan maps each file touched by later assistant snapshots to the snapshot
// tree it should be restored from.
func RevertPlan(db DB, sessionID schema.SessionID, messageID schema.MessageID) (map[schema.RelativePath]string, error) {
	var boundary int64
	err := db.QueryRow(
		"SELECT `seq` FROM `session_message` WHERE `session_id` = ? AND `id` = ?",
		string(sessionID), string(messageID),
	).Scan(&boundary)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &RevertMessageNotFoundError{SessionID: sessionID, MessageID: messageID}
		}
		return nil, err
	}

	rows, err := db.Query(
		"SELECT `id`,`session_id`,`type`,`seq`,`time_created`,`time_updated`,`data` FROM `session_message` "+
			"WHERE `session_id` = ? AND `type` = 'assistant' AND `seq` > ? ORDER BY `seq` ASC",
		string(sessionID), boundary,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := map[schema.RelativePath]string{}
	for rows.Next() {
		var row SessionMessageRow
		if err := rows.Scan(
			&row.ID,
			&row.SessionID,
			&row.Type,
			&row.Seq,
			&row.TimeCreated,
			&row.TimeUpdated,
			&row.Data,
		); err != nil {
			return nil, err
		}
		message, err := decodeMessageRow(row)
		if err != nil {
			return nil, err
		}
		assistant, ok := message.(schema.SessionMessageAssistant)
		if !ok || assistant.Snapshot == nil || assistant.Snapshot.Start == nil {
			continue
		}
		for _, file := range assistant.Snapshot.Files {
			if _, exists := files[file]; !exists {
				files[file] = *assistant.Snapshot.Start
			}
		}
	}
	return files, rows.Err()
}

// RevertStage records the files to restore and the snapshots to restore them from.
func RevertStage(
	db DB,
	events EventPublisher,
	snapshot RevertSnapshot,
	session schema.SessionInfo,
	messageID schema.MessageID,
	files *bool,
) (*schema.RevertState, error) {
	original, err := revertOriginalSnapshot(snapshot, session)
	if err != nil {
		return nil, err
	}

	next, err := RevertPlan(db, session.ID, messageID)
	if err != nil {
		return nil, err
	}

	restore := revertRestoreFiles(original, session, next, files)
	if len(restore) > 0 {
		if err := snapshot.Restore(restore); err != nil {
			return nil, err
		}
	}

	paths := revertPaths(next, files)
	diffFiles, trimmed, err := revertDiff(snapshot, original, paths)
	if err != nil {
		return nil, err
	}

	revert := &schema.RevertState{
		MessageID: messageID,
		Snapshot:  original,
		Diff:      &trimmed,
		Files:     diffFiles,
	}
	if _, err := events.Publish(schema.SessionEventRevertStaged, map[string]any{
		"sessionID": session.ID,
		"timestamp": time.Now().UnixMilli(),
		"revert":    revert,
	}); err != nil {
		return nil, err
	}
	return revert, nil
}

// revertOriginalSnapshot returns the snapshot to revert to, capturing one when
// none is staged.
func revertOriginalSnapshot(snapshot RevertSnapshot, session schema.SessionInfo) (*string, error) {
	if session.Revert != nil && session.Revert.Snapshot != nil {
		return session.Revert.Snapshot, nil
	}
	return snapshot.Capture()
}

// revertRestoreFiles maps each file to the tree it should be restored from.
func revertRestoreFiles(
	original *string,
	session schema.SessionInfo,
	next map[schema.RelativePath]string,
	files *bool,
) map[schema.RelativePath]string {
	restore := map[schema.RelativePath]string{}
	if original != nil && session.Revert != nil {
		for _, file := range session.Revert.Files {
			restore[file.Path] = *original
		}
	}
	if files == nil || *files {
		for file, tree := range next {
			restore[file] = tree
		}
	}
	return restore
}

func revertPaths(next map[schema.RelativePath]string, files *bool) []schema.RelativePath {
	paths := []schema.RelativePath{}
	if files == nil || *files {
		for file := range next {
			paths = append(paths, file)
		}
	}
	return paths
}

// revertDiff computes the staged diff and its concatenated, trimmed patch text.
func revertDiff(
	snapshot RevertSnapshot,
	original *string,
	paths []schema.RelativePath,
) ([]schema.RevertFileDiff, string, error) {
	if original == nil {
		return []schema.RevertFileDiff{}, "", nil
	}
	to := *original
	if captured, err := snapshot.Capture(); err == nil && captured != nil {
		to = *captured
	}
	diffFiles, err := snapshot.Diff(*original, to, paths)
	if err != nil {
		return nil, "", err
	}
	patch := ""
	for _, file := range diffFiles {
		patch += file.Patch
	}
	return diffFiles, strings.TrimSpace(patch), nil
}

// RevertClear drops a staged revert.
func RevertClear(events EventPublisher, snapshot RevertSnapshot, session schema.SessionInfo) error {
	if session.Revert == nil {
		return nil
	}
	if session.Revert.Snapshot != nil {
		files := map[schema.RelativePath]string{}
		for _, file := range session.Revert.Files {
			files[file.Path] = *session.Revert.Snapshot
		}
		if err := snapshot.Restore(files); err != nil {
			return err
		}
	}
	_, err := events.Publish(schema.SessionEventRevertCleared, map[string]any{
		"sessionID": session.ID,
		"timestamp": time.Now().UnixMilli(),
	})
	return err
}

// RevertCommit applies a staged revert and publishes the event.
func RevertCommit(events EventPublisher, session schema.SessionInfo) error {
	if session.Revert == nil {
		return nil
	}
	_, err := events.Publish(schema.SessionEventRevertCommitted, map[string]any{
		"sessionID": session.ID,
		"messageID": session.Revert.MessageID,
		"timestamp": time.Now().UnixMilli(),
	})
	return err
}
