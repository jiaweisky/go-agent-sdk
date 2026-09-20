package session

import (
	"database/sql"
	"errors"

	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/schema"
)

// LatestCompaction mirrors SessionHistory.latestCompaction.
func LatestCompaction(db *database.Database, sessionID schema.SessionID) (*int64, error) {
	return latestCompaction(db.DB, sessionID)
}

func latestCompaction(db DB, sessionID schema.SessionID) (*int64, error) {
	row := db.QueryRow(
		"SELECT `seq` FROM `session_message` WHERE `session_id` = ? AND `type` = 'compaction' ORDER BY `seq` DESC LIMIT 1",
		string(sessionID),
	)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &seq, nil
}

func contextEpochBaselineSeq(db *database.Database, sessionID schema.SessionID) (*int64, error) {
	row := db.DB.QueryRow(
		"SELECT `baseline_seq` FROM `session_context_epoch` WHERE `session_id` = ?",
		string(sessionID),
	)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &seq, nil
}

func messageRows(
	db *database.Database,
	sessionID schema.SessionID,
	compaction *int64,
	baselineSeq *int64,
) ([]SessionMessageRow, error) {
	query := "SELECT `id`,`session_id`,`type`,`seq`,`time_created`,`time_updated`,`data` FROM `session_message` WHERE `session_id` = ?"
	args := []any{string(sessionID)}
	if compaction != nil {
		if baselineSeq != nil {
			query += " AND (`seq` >= ? OR (`type` = 'system' AND `seq` > ?))"
			args = append(args, *compaction, *baselineSeq)
		} else {
			query += " AND `seq` >= ?"
			args = append(args, *compaction)
		}
	}
	if baselineSeq != nil {
		query += " AND (`type` != 'system' OR `seq` > ?)"
		args = append(args, *baselineSeq)
	}
	query += " ORDER BY `seq` ASC"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SessionMessageRow
	for rows.Next() {
		var row SessionMessageRow
		if err := rows.Scan(&row.ID, &row.SessionID, &row.Type, &row.Seq, &row.TimeCreated, &row.TimeUpdated, &row.Data); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func decodeMessageRow(row SessionMessageRow) (schema.SessionMessage, error) {
	message, err := schema.DecodeSessionMessageWithMeta([]byte(row.Data), schema.MessageID(row.ID), row.Type)
	if err != nil {
		return nil, &MessageDecodeError{
			SessionID: schema.SessionID(row.SessionID),
			MessageID: schema.MessageID(row.ID),
		}
	}
	return message, nil
}

func decodeMessageRows(rows []SessionMessageRow) ([]schema.SessionMessage, error) {
	result := make([]schema.SessionMessage, 0, len(rows))
	for _, row := range rows {
		message, err := decodeMessageRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, nil
}

// Load mirrors SessionHistory.load.
func Load(db *database.Database, sessionID schema.SessionID) ([]schema.SessionMessage, error) {
	baselineSeq, err := contextEpochBaselineSeq(db, sessionID)
	if err != nil {
		return nil, err
	}
	compaction, err := LatestCompaction(db, sessionID)
	if err != nil {
		return nil, err
	}
	rows, err := messageRows(db, sessionID, compaction, baselineSeq)
	if err != nil {
		return nil, err
	}
	return decodeMessageRows(rows)
}

// HistoryEntry mirrors SessionHistory.entriesForRunner entries.
type HistoryEntry struct {
	Seq     int64
	Message schema.SessionMessage
}

// EntriesForRunner mirrors SessionHistory.entriesForRunner.
func EntriesForRunner(db *database.Database, sessionID schema.SessionID, baselineSeq int64) ([]HistoryEntry, error) {
	compaction, err := LatestCompaction(db, sessionID)
	if err != nil {
		return nil, err
	}
	rows, err := messageRows(db, sessionID, compaction, &baselineSeq)
	if err != nil {
		return nil, err
	}
	result := make([]HistoryEntry, 0, len(rows))
	for _, row := range rows {
		message, err := decodeMessageRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, HistoryEntry{Seq: row.Seq, Message: message})
	}
	return result, nil
}

// LoadForRunner mirrors SessionHistory.loadForRunner.
func LoadForRunner(db *database.Database, sessionID schema.SessionID, baselineSeq int64) ([]schema.SessionMessage, error) {
	entries, err := EntriesForRunner(db, sessionID, baselineSeq)
	if err != nil {
		return nil, err
	}
	result := make([]schema.SessionMessage, len(entries))
	for i, entry := range entries {
		result[i] = entry.Message
	}
	return result, nil
}
