package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jiaweisky/go-agent-sdk/core/systemcontext"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// ContextEpochPrepared is a prepared epoch: its baseline and the sequence it was
// prepared at.
type ContextEpochPrepared struct {
	Baseline    string
	BaselineSeq int64
}

// ContextEpochPublisher is the event surface SessionContextEpoch.prepare needs.
// PublishWithCommit must run commit inside the durable event transaction and
// hand it the transaction handle, so the stored snapshot advances atomically
// with the ContextUpdated event and is not replayed.
type ContextEpochPublisher interface {
	PublishWithCommit(
		def schema.EventDefinition,
		data any,
		commit func(tx DB, seq int) error,
	) (*schema.EventPayloadDurable, error)
}

type contextEpochRow struct {
	SessionID   string
	Baseline    string
	Snapshot    string
	BaselineSeq int64
}

// ContextEpochInitialize creates the session's epoch; it returns nil when the
// session already has one.
func ContextEpochInitialize(
	db DB,
	context systemcontext.SystemContext,
	sessionID schema.SessionID,
) (*ContextEpochPrepared, error) {
	exists, err := contextEpochExists(db, sessionID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, nil
	}
	generation, err := systemcontext.Initialize(context)
	if err != nil {
		return nil, err
	}
	baselineSeq, err := contextEpochInsert(db, sessionID, generation)
	if err != nil {
		return nil, err
	}
	return &ContextEpochPrepared{Baseline: generation.Baseline, BaselineSeq: baselineSeq}, nil
}

// ContextEpochPrepare rebuilds the epoch from the session's current context.
func ContextEpochPrepare(
	db DB,
	events ContextEpochPublisher,
	context systemcontext.SystemContext,
	sessionID schema.SessionID,
) (ContextEpochPrepared, error) {
	stored, err := contextEpochFind(db, sessionID)
	if err != nil {
		return ContextEpochPrepared{}, err
	}
	compaction, err := latestCompaction(db, sessionID)
	if err != nil {
		return ContextEpochPrepared{}, err
	}
	if stored == nil {
		generation, err := systemcontext.Initialize(context)
		if err != nil {
			return ContextEpochPrepared{}, err
		}
		baselineSeq, err := contextEpochInsert(db, sessionID, generation)
		if err != nil {
			return ContextEpochPrepared{}, err
		}
		return ContextEpochPrepared{Baseline: generation.Baseline, BaselineSeq: baselineSeq}, nil
	}

	snapshot, err := systemcontext.DecodeSnapshot([]byte(stored.Snapshot))
	if err != nil {
		return ContextEpochPrepared{}, &ContextSnapshotDecodeError{SessionID: sessionID, Details: err.Error()}
	}
	var replacementSeq *int64
	if compaction != nil && *compaction > stored.BaselineSeq {
		replacementSeq = compaction
	}
	var result systemcontext.ReconcileResult
	// A replacement at sequence 0 falls through to reconcile: only a non-zero
	// sequence replaces the snapshot.
	if replacementSeq != nil && *replacementSeq != 0 {
		result, err = systemcontext.Replace(context, snapshot)
	} else {
		result, err = systemcontext.Reconcile(context, snapshot)
	}
	if err != nil {
		return ContextEpochPrepared{}, err
	}

	switch result.Tag {
	case "Unchanged", "ReplacementBlocked":
		return ContextEpochPrepared{Baseline: stored.Baseline, BaselineSeq: stored.BaselineSeq}, nil
	case "ReplacementReady":
		baselineSeq := int64(0)
		if replacementSeq != nil {
			baselineSeq = *replacementSeq
		} else {
			baselineSeq, err = latestEventSequence(db, sessionID)
			if err != nil {
				return ContextEpochPrepared{}, err
			}
		}
		if err := contextEpochReplace(db, sessionID, baselineSeq, *result.Generation); err != nil {
			return ContextEpochPrepared{}, err
		}
		return ContextEpochPrepared{Baseline: result.Generation.Baseline, BaselineSeq: baselineSeq}, nil
	}

	advance := result.Snapshot
	if _, err := events.PublishWithCommit(schema.SessionEventContextUpdated, map[string]any{
		"sessionID": sessionID,
		"messageID": schema.NewMessageID(),
		"timestamp": time.Now().UnixMilli(),
		"text":      result.Text,
	}, func(tx DB, seq int) error {
		return contextEpochAdvance(tx, sessionID, advance)
	}); err != nil {
		return ContextEpochPrepared{}, err
	}
	return ContextEpochPrepared{Baseline: stored.Baseline, BaselineSeq: stored.BaselineSeq}, nil
}

// ContextEpochReset drops the session's epoch.
func ContextEpochReset(db DB, sessionID schema.SessionID) error {
	_, err := db.Exec("DELETE FROM `session_context_epoch` WHERE `session_id` = ?", string(sessionID))
	return err
}

func contextEpochExists(db DB, sessionID schema.SessionID) (bool, error) {
	var stored string
	err := db.QueryRow("SELECT `session_id` FROM `session_context_epoch` WHERE `session_id` = ?", string(sessionID)).
		Scan(&stored)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func contextEpochFind(db DB, sessionID schema.SessionID) (*contextEpochRow, error) {
	var row contextEpochRow
	err := db.QueryRow(
		"SELECT `session_id`,`baseline`,`snapshot`,`baseline_seq` FROM `session_context_epoch` WHERE `session_id` = ?",
		string(sessionID),
	).Scan(&row.SessionID, &row.Baseline, &row.Snapshot, &row.BaselineSeq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func contextEpochInsert(db DB, sessionID schema.SessionID, generation systemcontext.Generation) (int64, error) {
	baselineSeq, err := latestEventSequence(db, sessionID)
	if err != nil {
		return 0, err
	}
	snapshot, err := json.Marshal(generation.Snapshot)
	if err != nil {
		return 0, err
	}
	_, err = db.Exec(
		"INSERT INTO `session_context_epoch` (`session_id`,`baseline`,`snapshot`,`baseline_seq`) VALUES (?,?,?,?)",
		string(sessionID), generation.Baseline, string(snapshot), baselineSeq,
	)
	if err != nil {
		return 0, err
	}
	return baselineSeq, nil
}

func contextEpochReplace(
	db DB,
	sessionID schema.SessionID,
	baselineSeq int64,
	generation systemcontext.Generation,
) error {
	snapshot, err := json.Marshal(generation.Snapshot)
	if err != nil {
		return err
	}
	result, err := db.Exec(
		"UPDATE `session_context_epoch` SET `baseline` = ?, `snapshot` = ?, `baseline_seq` = ? WHERE `session_id` = ?",
		generation.Baseline, string(snapshot), baselineSeq, string(sessionID),
	)
	if err != nil {
		return err
	}
	return requireEpochUpdated(result)
}

func contextEpochAdvance(db DB, sessionID schema.SessionID, snapshot systemcontext.Snapshot) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	result, err := db.Exec(
		"UPDATE `session_context_epoch` SET `snapshot` = ? WHERE `session_id` = ?",
		string(encoded), string(sessionID),
	)
	if err != nil {
		return err
	}
	return requireEpochUpdated(result)
}

func requireEpochUpdated(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("Context Epoch not found")
	}
	return nil
}

// latestEventSequence is the latest durable sequence for the aggregate, or -1
// when there is none.
func latestEventSequence(db DB, sessionID schema.SessionID) (int64, error) {
	var seq int64
	err := db.QueryRow("SELECT `seq` FROM `event_sequence` WHERE `aggregate_id` = ?", string(sessionID)).Scan(&seq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil
		}
		return -1, err
	}
	return seq, nil
}
