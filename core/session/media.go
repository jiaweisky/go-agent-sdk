package session

import (
	"time"

	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// PersistMessage rewrites a stored message's data. It is used after an
// attachment gained a resolved file reference, so a rebuilt turn or a restart
// reuses it instead of re-uploading.
func PersistMessage(db *database.Database, sessionID schema.SessionID, message schema.SessionMessage) error {
	if db == nil {
		return nil
	}
	data, err := v2MessageData(message)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(
		"UPDATE `session_message` SET `data` = ?, `time_updated` = ? WHERE `id` = ? AND `session_id` = ?",
		data, time.Now().UnixMilli(), string(sessionMessageID(message)), string(sessionID),
	)
	return err
}
