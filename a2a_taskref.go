package agent

import (
	"github.com/jiaweisky/go-agent-sdk/core/session"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// Turn metadata keys recording the remote task a local session is talking to,
// so a later turn can resume a remote task that parked at input-required. The
// names follow the ADK interop convention (docs/DESIGN-A2A.md §4.7).
const (
	turnMetaRemoteTask    = "a2a:task_id"
	turnMetaRemoteContext = "a2a:context_id"
)

// persistRemoteTask records (or clears, when taskID is empty) the remote task a
// session's turn is talking to. It writes turn metadata, which the runner
// preserves when it closes the turn. Only one turn carries the record: setting
// it clears it from the session's other turns, and clearing removes it from all,
// so a completed remote task is never resumed again.
func (r *runtime) persistRemoteTask(sessionID, turnID, taskID, contextID string) error {
	if r.db == nil {
		return nil
	}
	store := session.NewTurnStore(r.db)
	turns, err := store.ListForSession(schema.SessionID(sessionID))
	if err != nil {
		return err
	}
	target := schema.TurnID(turnID)
	for _, info := range turns {
		set := taskID != "" && info.ID == target
		hasRecord := info.Metadata != nil && info.Metadata[turnMetaRemoteTask] != nil
		if !set && !hasRecord {
			continue
		}
		if info.Metadata == nil {
			info.Metadata = map[string]any{}
		}
		if set {
			info.Metadata[turnMetaRemoteTask] = taskID
			info.Metadata[turnMetaRemoteContext] = contextID
		} else {
			delete(info.Metadata, turnMetaRemoteTask)
			delete(info.Metadata, turnMetaRemoteContext)
		}
		if err := store.Update(info); err != nil {
			return err
		}
	}
	return nil
}

// loadRemoteTask returns the most recent remote task recorded for a session, so
// a parked remote task is resumed rather than restarted.
func (r *runtime) loadRemoteTask(sessionID string) (string, string, bool) {
	if r.db == nil {
		return "", "", false
	}
	store := session.NewTurnStore(r.db)
	turns, err := store.ListForSession(schema.SessionID(sessionID))
	if err != nil {
		return "", "", false
	}
	for _, info := range turns {
		taskID, _ := info.Metadata[turnMetaRemoteTask].(string)
		if taskID == "" {
			continue
		}
		contextID, _ := info.Metadata[turnMetaRemoteContext].(string)
		return taskID, contextID, true
	}
	return "", "", false
}
