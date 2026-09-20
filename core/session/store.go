package session

import (
	"database/sql"
	"errors"

	"github.com/user/go-agent-sdk/core/database"
	"github.com/user/go-agent-sdk/schema"
)

// Store mirrors SessionStore.Service.
type Store struct {
	db *database.Database
}

func NewStore(db *database.Database) *Store {
	return &Store{db: db}
}

// Get mirrors SessionStore.get.
func (s *Store) Get(sessionID schema.SessionID) (*schema.SessionInfo, error) {
	row, err := scanSessionRow(s.db, sessionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	info := FromRow(*row)
	return &info, nil
}

// Context mirrors SessionStore.context.
func (s *Store) Context(sessionID schema.SessionID) ([]schema.SessionMessage, error) {
	return Load(s.db, sessionID)
}

// RunnerContext mirrors SessionStore.runnerContext.
func (s *Store) RunnerContext(sessionID schema.SessionID, baselineSeq int64) ([]schema.SessionMessage, error) {
	return LoadForRunner(s.db, sessionID, baselineSeq)
}

// StoreMessage mirrors SessionStore.message's return value.
type StoreMessage struct {
	SessionID schema.SessionID
	Message   schema.SessionMessage
}

// Message mirrors SessionStore.message.
func (s *Store) Message(messageID schema.MessageID) (*StoreMessage, error) {
	row := s.db.DB.QueryRow(
		"SELECT `id`,`session_id`,`type`,`seq`,`time_created`,`time_updated`,`data` FROM `session_message` WHERE `id` = ?",
		string(messageID),
	)
	var messageRow SessionMessageRow
	if err := row.Scan(
		&messageRow.ID, &messageRow.SessionID, &messageRow.Type, &messageRow.Seq,
		&messageRow.TimeCreated, &messageRow.TimeUpdated, &messageRow.Data,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	message, err := decodeMessageRow(messageRow)
	if err != nil {
		return nil, err
	}
	return &StoreMessage{SessionID: schema.SessionID(messageRow.SessionID), Message: message}, nil
}
