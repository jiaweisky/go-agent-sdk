package session

import (
	"database/sql"
	"errors"

	"github.com/jiaweisky/go-agent-sdk/internal/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// Store reads sessions, messages and their context from the database.
type Store struct {
	db *database.Database
}

func NewStore(db *database.Database) *Store {
	return &Store{db: db}
}

// Get returns a session's info, or nil when it does not exist.
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

// Context returns the session's messages with their parts.
func (s *Store) Context(sessionID schema.SessionID) ([]schema.SessionMessage, error) {
	return Load(s.db, sessionID)
}

// Messages returns every stored message for a session, ignoring compaction and
// the context-epoch baseline (the full transcript, for HTTP hosts).
func (s *Store) Messages(sessionID schema.SessionID) ([]schema.SessionMessage, error) {
	rows, err := messageRows(s.db, sessionID, nil, nil)
	if err != nil {
		return nil, err
	}
	return decodeMessageRows(rows)
}

// RunnerContext returns the messages after a baseline sequence.
func (s *Store) RunnerContext(sessionID schema.SessionID, baselineSeq int64) ([]schema.SessionMessage, error) {
	return LoadForRunner(s.db, sessionID, baselineSeq)
}

// StoreMessage is a stored message with its parts and sequence.
type StoreMessage struct {
	SessionID schema.SessionID
	Message   schema.SessionMessage
}

// Message returns one stored message, or nil.
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
