package session

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jiaweisky/go-agent-sdk/core/database"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TurnStore persists core turn records. A turn is one terminal-bound unit of
// work in a session: the local subagent's delegated run or an A2A task.
type TurnStore struct {
	db *database.Database
}

func NewTurnStore(db *database.Database) *TurnStore {
	return &TurnStore{db: db}
}

// Create inserts a new turn.
func (s *TurnStore) Create(info schema.TurnInfo) error {
	metadata, err := encodeTurnMetadata(info.Metadata)
	if err != nil {
		return err
	}
	model, err := encodeTurnModel(info.Model)
	if err != nil {
		return err
	}
	_, err = s.db.DB.Exec(
		"INSERT INTO `turn` (`id`,`session_id`,`parent_turn_id`,`input_id`,`state`,`pending_request_id`,`agent`,`model`,`anchor_seq`,`end_seq`,`metadata`,`time_created`,`time_updated`) "+
			"VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
		string(info.ID),
		string(info.SessionID),
		turnStringPtr(info.ParentTurnID),
		turnStringPtr(info.InputID),
		string(info.State),
		info.PendingRequestID,
		turnStringPtr(info.Agent),
		model,
		info.AnchorSeq,
		info.EndSeq,
		metadata,
		info.TimeCreated,
		info.TimeUpdated,
	)
	return err
}

// Update replaces a turn's mutable fields.
func (s *TurnStore) Update(info schema.TurnInfo) error {
	metadata, err := encodeTurnMetadata(info.Metadata)
	if err != nil {
		return err
	}
	model, err := encodeTurnModel(info.Model)
	if err != nil {
		return err
	}
	res, err := s.db.DB.Exec(
		"UPDATE `turn` SET `state`=?,`pending_request_id`=?,`agent`=?,`model`=?,`anchor_seq`=?,`end_seq`=?,`metadata`=?,`time_updated`=? WHERE `id`=?",
		string(info.State),
		info.PendingRequestID,
		turnStringPtr(info.Agent),
		model,
		info.AnchorSeq,
		info.EndSeq,
		metadata,
		info.TimeUpdated,
		string(info.ID),
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return errTurnNotFound
	}
	return nil
}

// Get returns a turn by id, or nil when it does not exist.
func (s *TurnStore) Get(id schema.TurnID) (*schema.TurnInfo, error) {
	row := s.db.DB.QueryRow(turnSelect+" WHERE `id` = ?", string(id))
	info, err := scanTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// ActiveForSession returns the session's most recent active turn (running or
// awaiting input), or nil when the session is idle.
func (s *TurnStore) ActiveForSession(sessionID schema.SessionID) (*schema.TurnInfo, error) {
	row := s.db.DB.QueryRow(
		turnSelect+" WHERE `session_id` = ? AND `state` IN (?,?) ORDER BY `time_created` DESC LIMIT 1",
		string(sessionID), string(schema.TurnStateRunning), string(schema.TurnStateAwaitingInput),
	)
	info, err := scanTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// ListForSession returns a session's turns, newest first.
func (s *TurnStore) ListForSession(sessionID schema.SessionID) ([]schema.TurnInfo, error) {
	rows, err := s.db.DB.Query(turnSelect+" WHERE `session_id` = ? ORDER BY `time_created` DESC", string(sessionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurns(rows)
}

// ListAll returns every turn, newest first.
func (s *TurnStore) ListAll() ([]schema.TurnInfo, error) {
	rows, err := s.db.DB.Query(turnSelect + " ORDER BY `time_created` DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurns(rows)
}

// FindByMetadata returns the turns whose metadata value for key equals value,
// newest first. The key is matched literally, so dotted keys such as
// "a2a.context_id" are read as a single flat metadata key.
func (s *TurnStore) FindByMetadata(key, value string) ([]schema.TurnInfo, error) {
	rows, err := s.db.DB.Query(
		turnSelect+" WHERE `metadata` IS NOT NULL AND json_extract(`metadata`, ?) = ? ORDER BY `time_created` DESC",
		jsonPath(key), value,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTurns(rows)
}

// jsonPath renders a flat metadata key as a quoted SQLite JSON path.
func jsonPath(key string) string {
	return `$."` + key + `"`
}

const turnSelect = "SELECT `id`,`session_id`,`parent_turn_id`,`input_id`,`state`,`pending_request_id`,`agent`,`model`,`anchor_seq`,`end_seq`,`metadata`,`time_created`,`time_updated` FROM `turn`"

func scanTurn(row rowScanner) (schema.TurnInfo, error) {
	var (
		info            schema.TurnInfo
		id, session, st string
		parent, input   *string
		pending, agent  *string
		model, metadata *string
		anchor, end     *int64
	)
	if err := row.Scan(
		&id,
		&session,
		&parent,
		&input,
		&st,
		&pending,
		&agent,
		&model,
		&anchor,
		&end,
		&metadata,
		&info.TimeCreated,
		&info.TimeUpdated,
	); err != nil {
		return schema.TurnInfo{}, err
	}
	info.ID = schema.TurnID(id)
	info.SessionID = schema.SessionID(session)
	if parent != nil {
		v := schema.TurnID(*parent)
		info.ParentTurnID = &v
	}
	if input != nil {
		v := schema.MessageID(*input)
		info.InputID = &v
	}
	info.State = schema.TurnState(st)
	info.PendingRequestID = pending
	if agent != nil {
		v := schema.AgentID(*agent)
		info.Agent = &v
	}
	if model != nil && *model != "" {
		var ref schema.ModelRef
		if err := json.Unmarshal([]byte(*model), &ref); err == nil {
			info.Model = &ref
		}
	}
	info.AnchorSeq = anchor
	info.EndSeq = end
	md, err := decodeTurnMetadata(metadata)
	if err != nil {
		return schema.TurnInfo{}, err
	}
	info.Metadata = md
	return info, nil
}

func scanTurns(rows *sql.Rows) ([]schema.TurnInfo, error) {
	var out []schema.TurnInfo
	for rows.Next() {
		info, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

func encodeTurnMetadata(metadata map[string]any) (*string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	return jsonStringPtr(metadata)
}

func encodeTurnModel(model *schema.ModelRef) (*string, error) {
	if model == nil {
		return nil, nil
	}
	return jsonStringPtr(model)
}

// jsonStringPtr marshals a value into a JSON string pointer.
func jsonStringPtr(value any) (*string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

func decodeTurnMetadata(metadata *string) (map[string]any, error) {
	if metadata == nil || *metadata == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(*metadata), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func turnStringPtr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

// errTurnNotFound is returned by Update when the turn row is missing.
var errTurnNotFound = errors.New("turn not found")

// LatestSeq returns the highest session-message sequence for a session.
func (s *TurnStore) LatestSeq(sessionID schema.SessionID) (int64, error) {
	return LatestMessageSeq(s.db, sessionID)
}

// LatestMessageSeq returns the highest session-message sequence for a session,
// used as a turn's anchor (start) or end sequence.
func LatestMessageSeq(db *database.Database, sessionID schema.SessionID) (int64, error) {
	var seq int64
	err := db.DB.QueryRow(
		"SELECT COALESCE(MAX(`seq`), 0) FROM `session_message` WHERE `session_id` = ?",
		string(sessionID),
	).Scan(&seq)
	return seq, err
}

// LatestInputID returns the most recently admitted session input's id, the
// trigger a turn started from.
func LatestInputID(db *database.Database, sessionID schema.SessionID) (*schema.MessageID, error) {
	var id string
	err := db.DB.QueryRow(
		"SELECT `id` FROM `session_input` WHERE `session_id` = ? ORDER BY `admitted_seq` DESC LIMIT 1",
		string(sessionID),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	messageID := schema.MessageID(id)
	return &messageID, nil
}
