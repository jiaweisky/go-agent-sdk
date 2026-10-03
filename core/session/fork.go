package session

import (
	"encoding/json"

	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/schema"
)

// Fork copies the source session's messages into the target session, giving each
// copy a fresh message id. Copying stops before messageID when it names a
// message in the source; an empty or unknown id copies the whole history. The
// target session must already exist and have no messages of its own.
func Fork(db *database.Database, source, target schema.SessionID, messageID schema.MessageID) error {
	rows, err := messageRows(db, source, nil, nil)
	if err != nil {
		return err
	}
	rows = rows[:forkLimit(rows, messageID)]
	if len(rows) == 0 {
		return nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, row := range rows {
		if err := copyMessageRow(tx, row, target, int64(i+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// copyMessageRow writes one source row into the target session under a fresh
// message id and sequence.
func copyMessageRow(tx DB, row SessionMessageRow, target schema.SessionID, seq int64) error {
	data := row.Data
	if row.Type == "synthetic" {
		rebound, err := rebindSessionID(row.Data, target)
		if err != nil {
			return err
		}
		data = rebound
	}
	return insertMessageRow(
		tx, string(schema.NewMessageID()), string(target), row.Type, seq,
		row.TimeCreated, row.TimeUpdated, data,
	)
}

// forkLimit returns how many rows to copy: the position of messageID when it is
// found, otherwise every row.
func forkLimit(rows []SessionMessageRow, messageID schema.MessageID) int {
	if messageID == "" {
		return len(rows)
	}
	for i, row := range rows {
		if schema.MessageID(row.ID) == messageID {
			return i
		}
	}
	return len(rows)
}

// rebindSessionID repoints a copied message's embedded session id at the target,
// leaving every other field as raw JSON so nothing is reformatted.
func rebindSessionID(data string, target schema.SessionID) (string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &object); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(string(target))
	if err != nil {
		return "", err
	}
	object["sessionID"] = encoded
	result, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(result), nil
}
