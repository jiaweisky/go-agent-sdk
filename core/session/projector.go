package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/jiaweisky/go-agent-sdk/schema"
	v1 "github.com/jiaweisky/go-agent-sdk/schema/v1"
)

// ProjectRegistrar is the narrow event-registration surface the projector needs.
type ProjectRegistrar interface {
	Project(def schema.EventDefinition, projector func(schema.EventPayload, DB) error) error
}

// SessionAlreadyProjected reports an event that was already projected.
type SessionAlreadyProjected struct{}

func (e *SessionAlreadyProjected) Error() string {
	return "session already projected"
}

// RegisterProjectors wires the session projectors into the event service.
func RegisterProjectors(events ProjectRegistrar) error {
	projectors := []struct {
		def       schema.EventDefinition
		projector func(schema.EventPayload, DB) error
	}{
		{v1.V1EventCreated, projectV1Created},
		{v1.V1EventUpdated, projectV1Updated},
		{v1.V1EventDeleted, projectV1Deleted},
		{v1.V1EventMessageUpdated, projectV1MessageUpdated},
		{v1.V1EventMessageRemoved, projectV1MessageRemoved},
		{v1.V1EventPartUpdated, projectV1PartUpdated},
		{v1.V1EventPartRemoved, projectV1PartRemoved},
		{schema.SessionEventMoved, projectMoved},
		{schema.SessionEventAgentSwitched, projectAgentSwitched},
		{schema.SessionEventModelSwitched, projectModelSwitched},
		{schema.SessionEventTitleUpdated, projectTitleUpdated},
		{schema.SessionEventPrompted, projectPrompted},
		{schema.SessionEventPromptAdmitted, projectPromptAdmitted},
		{schema.SessionEventContextUpdated, runProjector},
		{schema.SessionEventSynthetic, runProjector},
		{schema.SessionEventShellStarted, runProjector},
		{schema.SessionEventShellEnded, runProjector},
		{schema.SessionEventStepStarted, runProjector},
		{schema.SessionEventStepEnded, runProjector},
		{schema.SessionEventStepFailed, runProjector},
		{schema.SessionEventTextStarted, runProjector},
		{schema.SessionEventTextEnded, runProjector},
		{schema.SessionEventToolInputStarted, runProjector},
		{schema.SessionEventToolInputEnded, runProjector},
		{schema.SessionEventToolCalled, runProjector},
		{schema.SessionEventToolProgress, runProjector},
		{schema.SessionEventToolSuccess, runProjector},
		{schema.SessionEventToolFailed, runProjector},
		{schema.SessionEventReasoningStarted, runProjector},
		{schema.SessionEventReasoningEnded, runProjector},
		{schema.SessionEventCompactionEnded, runProjector},
		{schema.SessionEventRevertStaged, projectRevertStaged},
		{schema.SessionEventRevertCleared, projectRevertCleared},
		{schema.SessionEventRevertCommitted, projectRevertCommitted},
	}
	for _, entry := range projectors {
		if err := events.Project(entry.def, entry.projector); err != nil {
			return err
		}
	}
	return nil
}

var sessionColumns = []string{
	"id", "project_id", "workspace_id", "parent_id", "slug", "directory", "path", "title", "agent", "model",
	"version", "share_url", "summary_additions", "summary_deletions", "summary_files", "summary_diffs", "metadata",
	"cost", "tokens_input", "tokens_output", "tokens_reasoning", "tokens_cache_read", "tokens_cache_write",
	"revert", "permission", "time_created", "time_updated", "time_compacting", "time_archived",
}

func jsonOrNull(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface:
		if reflected.IsNil() {
			return nil, nil
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func sessionValues(info v1.V1SessionInfo) ([]any, error) {
	model, err := jsonOrNull(info.Model)
	if err != nil {
		return nil, err
	}
	revert, err := jsonOrNull(info.Revert)
	if err != nil {
		return nil, err
	}
	permission, err := jsonOrNull(info.Permission)
	if err != nil {
		return nil, err
	}
	metadata, err := jsonOrNull(info.Metadata)
	if err != nil {
		return nil, err
	}
	var summaryDiffs any
	if info.Summary != nil && info.Summary.Diffs != nil {
		summaryDiffs, err = jsonOrNull(info.Summary.Diffs)
		if err != nil {
			return nil, err
		}
	}
	var summaryAdditions, summaryDeletions, summaryFiles any
	if info.Summary != nil {
		summaryAdditions = info.Summary.Additions
		summaryDeletions = info.Summary.Deletions
		summaryFiles = info.Summary.Files
	}
	var shareURL any
	if info.Share != nil {
		shareURL = info.Share.URL
	}
	cost := float64(0)
	if info.Cost != nil {
		cost = *info.Cost
	}
	var tokensInput, tokensOutput, tokensReasoning, tokensCacheRead, tokensCacheWrite any = float64(0), float64(0), float64(0), float64(0), float64(0)
	if info.Tokens != nil {
		tokensInput = info.Tokens.Input
		tokensOutput = info.Tokens.Output
		tokensReasoning = info.Tokens.Reasoning
		tokensCacheRead = info.Tokens.Cache.Read
		tokensCacheWrite = info.Tokens.Cache.Write
	}
	var timeCompacting, timeArchived any
	if info.Time.Compacting != nil {
		timeCompacting = *info.Time.Compacting
	}
	if info.Time.Archived != nil {
		timeArchived = *info.Time.Archived
	}
	return []any{
		string(info.ID), string(info.ProjectID), info.WorkspaceID, info.ParentID, info.Slug, info.Directory,
		info.Path, info.Title, info.Agent, model, info.Version, shareURL,
		summaryAdditions, summaryDeletions, summaryFiles, summaryDiffs, metadata, cost,
		tokensInput, tokensOutput, tokensReasoning, tokensCacheRead, tokensCacheWrite,
		revert, permission, info.Time.Created, info.Time.Updated, timeCompacting, timeArchived,
	}, nil
}

func insertSession(tx DB, info v1.V1SessionInfo) (bool, error) {
	values, err := sessionValues(info)
	if err != nil {
		return false, err
	}
	placeholders := ""
	for i := range sessionColumns {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}
	query := "INSERT INTO `session` (`" + joinColumns(sessionColumns) + "`) VALUES (" + placeholders + ") ON CONFLICT DO NOTHING"
	result, err := tx.Exec(query, values...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// sessionUpdateSet builds the columns to update from info: explicit null/zero
// fields are always written, absent optional fields are omitted so the existing
// column is kept.
func sessionUpdateSet(info v1.V1SessionInfo) (map[string]any, error) {
	cost := float64(0)
	if info.Cost != nil {
		cost = *info.Cost
	}
	values := map[string]any{
		"project_id":         string(info.ProjectID),
		"slug":               info.Slug,
		"directory":          info.Directory,
		"title":              info.Title,
		"version":            info.Version,
		"time_created":       info.Time.Created,
		"time_updated":       info.Time.Updated,
		"workspace_id":       info.WorkspaceID,
		"cost":               cost,
		"tokens_input":       float64(0),
		"tokens_output":      float64(0),
		"tokens_reasoning":   float64(0),
		"tokens_cache_read":  float64(0),
		"tokens_cache_write": float64(0),
		"revert":             nil,
	}
	if info.Tokens != nil {
		values["tokens_input"] = info.Tokens.Input
		values["tokens_output"] = info.Tokens.Output
		values["tokens_reasoning"] = info.Tokens.Reasoning
		values["tokens_cache_read"] = info.Tokens.Cache.Read
		values["tokens_cache_write"] = info.Tokens.Cache.Write
	}
	if err := applyOptionalSessionFields(values, info); err != nil {
		return nil, err
	}
	return values, nil
}

// applyOptionalSessionFields writes the optional columns present on info.
func applyOptionalSessionFields(values map[string]any, info v1.V1SessionInfo) error {
	if info.Revert != nil {
		if err := setJSONOrNull(values, "revert", info.Revert); err != nil {
			return err
		}
	}
	if info.ParentID != nil {
		values["parent_id"] = string(*info.ParentID)
	}
	if info.Agent != nil {
		values["agent"] = *info.Agent
	}
	if info.Model != nil {
		if err := setJSONOrNull(values, "model", info.Model); err != nil {
			return err
		}
	}
	if info.Share != nil {
		values["share_url"] = info.Share.URL
	}
	if info.Summary != nil {
		values["summary_additions"] = info.Summary.Additions
		values["summary_deletions"] = info.Summary.Deletions
		values["summary_files"] = info.Summary.Files
		if info.Summary.Diffs != nil {
			if err := setJSONOrNull(values, "summary_diffs", info.Summary.Diffs); err != nil {
				return err
			}
		}
	}
	if info.Metadata != nil {
		if err := setJSONOrNull(values, "metadata", info.Metadata); err != nil {
			return err
		}
	}
	if info.Permission != nil {
		if err := setJSONOrNull(values, "permission", info.Permission); err != nil {
			return err
		}
	}
	if info.Time.Compacting != nil {
		values["time_compacting"] = *info.Time.Compacting
	}
	if info.Time.Archived != nil {
		values["time_archived"] = *info.Time.Archived
	}
	return nil
}

// setJSONOrNull sets values[key] to the JSON encoding of value, or SQL NULL when
// value is nil.
func setJSONOrNull(values map[string]any, key string, value any) error {
	encoded, err := jsonOrNull(value)
	if err != nil {
		return err
	}
	values[key] = encoded
	return nil
}

func updateSession(tx DB, info v1.V1SessionInfo) error {
	values, err := sessionUpdateSet(info)
	if err != nil {
		return err
	}
	columns := make([]string, 0, len(values))
	for column := range values {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	set := ""
	args := make([]any, 0, len(columns)+1)
	for i, column := range columns {
		if i > 0 {
			set += ", "
		}
		set += "`" + column + "` = ?"
		args = append(args, values[column])
	}
	args = append(args, string(info.ID))
	_, err = tx.Exec("UPDATE `session` SET "+set+" WHERE `id` = ?", args...)
	return err
}

func joinColumns(columns []string) string {
	result := ""
	for i, column := range columns {
		if i > 0 {
			result += "`,`"
		}
		result += column
	}
	return result
}

func applyUsage(tx DB, sessionID schema.SessionID, value usage, sign float64) error {
	_, err := tx.Exec(
		"UPDATE `session` SET `cost` = `cost` + ?, `tokens_input` = `tokens_input` + ?, "+
			"`tokens_output` = `tokens_output` + ?, `tokens_reasoning` = `tokens_reasoning` + ?, "+
			"`tokens_cache_read` = `tokens_cache_read` + ?, `tokens_cache_write` = `tokens_cache_write` + ?, "+
			"`time_updated` = `time_updated` WHERE `id` = ?",
		value.Cost*sign, value.Tokens.Input*sign, value.Tokens.Output*sign, value.Tokens.Reasoning*sign,
		value.Tokens.CacheRead*sign, value.Tokens.CacheWrite*sign, string(sessionID),
	)
	return err
}

type usageTokens struct {
	Input      float64
	Output     float64
	Reasoning  float64
	CacheRead  float64
	CacheWrite float64
}

type usage struct {
	Cost   float64
	Tokens usageTokens
}

func parseUsage(part map[string]any) *usage {
	if part == nil || part["type"] != "step-finish" {
		return nil
	}
	cost, ok := part["cost"].(float64)
	if !ok {
		return nil
	}
	tokens, ok := part["tokens"].(map[string]any)
	if !ok {
		return nil
	}
	number := func(object map[string]any, key string) float64 {
		if value, ok := object[key].(float64); ok {
			return value
		}
		return 0
	}
	cache, _ := tokens["cache"].(map[string]any)
	return &usage{
		Cost: cost,
		Tokens: usageTokens{
			Input:      number(tokens, "input"),
			Output:     number(tokens, "output"),
			Reasoning:  number(tokens, "reasoning"),
			CacheRead:  number(cache, "read"),
			CacheWrite: number(cache, "write"),
		},
	}
}

func v2MessageData(message schema.SessionMessage) (string, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return "", err
	}
	delete(object, "id")
	delete(object, "type")
	data, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func stripObjectKeys(value map[string]any, keys ...string) (string, error) {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	for _, key := range keys {
		delete(clone, key)
	}
	data, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func sessionMessageCreated(message schema.SessionMessage) int64 {
	switch m := message.(type) {
	case schema.SessionMessageAgentSwitched:
		return int64(m.Time.Created)
	case schema.SessionMessageModelSwitched:
		return int64(m.Time.Created)
	case schema.SessionMessageUser:
		return int64(m.Time.Created)
	case schema.SessionMessageSynthetic:
		return int64(m.Time.Created)
	case schema.SessionMessageSystem:
		return int64(m.Time.Created)
	case schema.SessionMessageShell:
		return int64(m.Time.Created)
	case schema.SessionMessageAssistant:
		return int64(m.Time.Created)
	case schema.SessionMessageCompaction:
		return int64(m.Time.Created)
	}
	return 0
}

func eventSessionID(event schema.EventPayload) schema.SessionID {
	// event.Data may be a typed struct (in-process publish) or a decoded map
	// (replay); a JSON round-trip handles both.
	var probe struct {
		SessionID schema.SessionID `json:"sessionID"`
	}
	encoded, err := json.Marshal(event.Data)
	if err != nil {
		return ""
	}
	if err := json.Unmarshal(encoded, &probe); err != nil {
		return ""
	}
	return probe.SessionID
}

// projectorAdapter adapts the database to the projector's storage surface.
type projectorAdapter struct {
	tx    DB
	event schema.EventPayload
}

func (a *projectorAdapter) queryMessages(where string, args ...any) ([]SessionMessageRow, error) {
	rows, err := a.tx.Query(
		"SELECT `id`,`session_id`,`type`,`seq`,`time_created`,`time_updated`,`data` FROM `session_message` WHERE "+where,
		args...,
	)
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

func (a *projectorAdapter) GetCurrentAssistant() (*schema.SessionMessageAssistant, bool) {
	rows, err := a.queryMessages(
		"`session_id` = ? AND `type` = 'assistant' ORDER BY `seq` DESC LIMIT 1",
		string(eventSessionID(a.event)),
	)
	if err != nil || len(rows) == 0 {
		return nil, false
	}
	message, err := decodeMessageRow(rows[0])
	if err != nil {
		return nil, false
	}
	assistant, ok := message.(schema.SessionMessageAssistant)
	if !ok || assistant.Time.Completed != nil {
		return nil, false
	}
	return &assistant, true
}

func (a *projectorAdapter) GetAssistant(messageID schema.MessageID) (*schema.SessionMessageAssistant, bool) {
	rows, err := a.queryMessages(
		"`id` = ? AND `session_id` = ? AND `type` = 'assistant'",
		string(messageID), string(eventSessionID(a.event)),
	)
	if err != nil || len(rows) == 0 {
		return nil, false
	}
	message, err := decodeMessageRow(rows[0])
	if err != nil {
		return nil, false
	}
	assistant, ok := message.(schema.SessionMessageAssistant)
	if !ok {
		return nil, false
	}
	return &assistant, true
}

func (a *projectorAdapter) GetCurrentShell(callID string) (*schema.SessionMessageShell, bool) {
	rows, err := a.queryMessages(
		"`session_id` = ? AND `type` = 'shell' ORDER BY `seq` DESC",
		string(eventSessionID(a.event)),
	)
	if err != nil {
		return nil, false
	}
	for _, row := range rows {
		message, err := decodeMessageRow(row)
		if err != nil {
			continue
		}
		if shell, ok := message.(schema.SessionMessageShell); ok && shell.CallID == callID {
			return &shell, true
		}
	}
	return nil, false
}

func (a *projectorAdapter) updateMessage(message schema.SessionMessage) error {
	durable := a.event.Durable
	if durable == nil {
		return errors.New("Durable Session event is missing aggregate sequence")
	}
	data, err := v2MessageData(message)
	if err != nil {
		return err
	}
	_, err = a.tx.Exec(
		"UPDATE `session_message` SET `type` = ?, `time_created` = ?, `data` = ? WHERE `id` = ? AND `session_id` = ?",
		message.SessionMessageType(), sessionMessageCreated(message), data,
		string(sessionMessageID(message)), string(eventSessionID(a.event)),
	)
	return err
}

func (a *projectorAdapter) UpdateAssistant(assistant schema.SessionMessageAssistant) error {
	return a.updateMessage(assistant)
}

func (a *projectorAdapter) UpdateShell(shell schema.SessionMessageShell) error {
	return a.updateMessage(shell)
}

func (a *projectorAdapter) AppendMessage(message schema.SessionMessage) error {
	durable := a.event.Durable
	if durable == nil {
		return errors.New("Durable Session event is missing aggregate sequence")
	}
	data, err := v2MessageData(message)
	if err != nil {
		return err
	}
	created := sessionMessageCreated(message)
	return insertMessageRow(
		a.tx, string(sessionMessageID(message)), string(eventSessionID(a.event)), message.SessionMessageType(),
		int64(durable.Seq), created, created, data,
	)
}

// insertMessageRow writes one session_message row.
func insertMessageRow(tx DB, id, sessionID, messageType string, seq, created, updated int64, data string) error {
	_, err := tx.Exec(
		"INSERT INTO `session_message` (`id`,`session_id`,`type`,`seq`,`time_created`,`time_updated`,`data`) VALUES (?,?,?,?,?,?,?)",
		id, sessionID, messageType, seq, created, updated, data,
	)
	return err
}

func runProjector(event schema.EventPayload, tx DB) error {
	return Update(&projectorAdapter{tx: tx, event: event}, event)
}

func projectV1Created(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1SessionCreatedData](event.Data)
	if err != nil {
		return err
	}
	stored, err := insertSession(tx, data.Info)
	if err != nil {
		return err
	}
	if !stored {
		return &SessionAlreadyProjected{}
	}
	if data.Info.WorkspaceID != nil {
		if _, err := tx.Exec("UPDATE `workspace` SET `time_used` = ? WHERE `id` = ?", time.Now().UnixMilli(), string(*data.Info.WorkspaceID)); err != nil {
			return err
		}
	}
	return nil
}

func projectV1Updated(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1SessionCreatedData](event.Data)
	if err != nil {
		return err
	}
	return updateSession(tx, data.Info)
}

func projectV1Deleted(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1SessionCreatedData](event.Data)
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM `session` WHERE `id` = ?", string(data.SessionID))
	return err
}

func projectV1MessageUpdated(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1MessageUpdatedData](event.Data)
	if err != nil {
		return err
	}
	body, err := stripObjectKeys(data.Info, "id", "sessionID")
	if err != nil {
		return err
	}
	created, _ := data.Info["time"].(map[string]any)
	timeCreated := float64(0)
	if created != nil {
		if value, ok := created["created"].(float64); ok {
			timeCreated = value
		}
	}
	id, _ := data.Info["id"].(string)
	sessionID, _ := data.Info["sessionID"].(string)
	_, err = tx.Exec(
		"INSERT INTO `message` (`id`,`session_id`,`time_created`,`time_updated`,`data`) VALUES (?,?,?,?,?) "+
			"ON CONFLICT(`id`) DO UPDATE SET `data` = excluded.`data`",
		id, sessionID, int64(timeCreated), int64(timeCreated), body,
	)
	return err
}

func projectV1MessageRemoved(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1MessageRemovedData](event.Data)
	if err != nil {
		return err
	}
	rows, err := tx.Query("SELECT `data` FROM `part` WHERE `message_id` = ? AND `session_id` = ?", string(data.MessageID), string(data.SessionID))
	if err != nil {
		return err
	}
	var parts []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var part map[string]any
		if err := json.Unmarshal([]byte(raw), &part); err == nil {
			parts = append(parts, part)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, part := range parts {
		if previous := parseUsage(part); previous != nil {
			if err := applyUsage(tx, data.SessionID, *previous, -1); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec("DELETE FROM `message` WHERE `id` = ? AND `session_id` = ?", string(data.MessageID), string(data.SessionID))
	return err
}

func projectV1PartRemoved(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1PartRemovedData](event.Data)
	if err != nil {
		return err
	}
	var raw string
	err = tx.QueryRow("SELECT `data` FROM `part` WHERE `id` = ? AND `session_id` = ?", string(data.PartID), string(data.SessionID)).Scan(&raw)
	if err == nil {
		var part map[string]any
		if json.Unmarshal([]byte(raw), &part) == nil {
			if previous := parseUsage(part); previous != nil {
				if err := applyUsage(tx, data.SessionID, *previous, -1); err != nil {
					return err
				}
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec("DELETE FROM `part` WHERE `id` = ? AND `session_id` = ?", string(data.PartID), string(data.SessionID))
	return err
}

func projectV1PartUpdated(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[v1.V1PartUpdatedData](event.Data)
	if err != nil {
		return err
	}
	body, err := stripObjectKeys(data.Part, "id", "messageID", "sessionID")
	if err != nil {
		return err
	}
	id, _ := data.Part["id"].(string)
	messageID, _ := data.Part["messageID"].(string)
	sessionID, _ := data.Part["sessionID"].(string)
	var previousRaw, previousSessionID string
	previousErr := tx.QueryRow("SELECT `session_id`, `data` FROM `part` WHERE `id` = ?", id).Scan(&previousSessionID, &previousRaw)
	_, err = tx.Exec(
		"INSERT INTO `part` (`id`,`message_id`,`session_id`,`time_created`,`time_updated`,`data`) VALUES (?,?,?,?,?,?) "+
			"ON CONFLICT(`id`) DO UPDATE SET `data` = excluded.`data`",
		id, messageID, sessionID, int64(data.Time), int64(data.Time), body,
	)
	if err != nil {
		return err
	}
	if previousErr == nil {
		var previous map[string]any
		if json.Unmarshal([]byte(previousRaw), &previous) == nil {
			if value := parseUsage(previous); value != nil {
				if err := applyUsage(tx, schema.SessionID(previousSessionID), *value, -1); err != nil {
					return err
				}
			}
		}
	}
	if next := parseUsage(data.Part); next != nil {
		if err := applyUsage(tx, schema.SessionID(sessionID), *next, 1); err != nil {
			return err
		}
	}
	return nil
}

func projectMoved(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventMovedData](event.Data)
	if err != nil {
		return err
	}
	var workspaceID any
	if data.Location.WorkspaceID != nil {
		workspaceID = string(*data.Location.WorkspaceID)
	}
	var subdirectory any
	if data.Subdirectory != nil {
		subdirectory = string(*data.Subdirectory)
	}
	_, err = tx.Exec(
		"UPDATE `session` SET `directory` = ?, `path` = ?, `workspace_id` = ?, `time_updated` = ? WHERE `id` = ?",
		string(data.Location.Directory), subdirectory, workspaceID, int64(data.Timestamp), string(data.SessionID),
	)
	return err
}

func projectAgentSwitched(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventAgentSwitchedData](event.Data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE `session` SET `agent` = ?, `time_updated` = ? WHERE `id` = ?",
		data.Agent, int64(data.Timestamp), string(data.SessionID),
	); err != nil {
		return err
	}
	return runProjector(event, tx)
}

func projectModelSwitched(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventModelSwitchedData](event.Data)
	if err != nil {
		return err
	}
	model, err := jsonOrNull(data.Model)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE `session` SET `model` = ?, `time_updated` = ? WHERE `id` = ?",
		model, int64(data.Timestamp), string(data.SessionID),
	); err != nil {
		return err
	}
	return runProjector(event, tx)
}

func projectTitleUpdated(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventTitleUpdatedData](event.Data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		"UPDATE `session` SET `title` = ?, `time_updated` = ? WHERE `id` = ?",
		data.Title, int64(data.Timestamp), string(data.SessionID),
	)
	return err
}

func projectPrompted(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventPromptData](event.Data)
	if err != nil {
		return err
	}
	durable := event.Durable
	if durable == nil {
		return errors.New("Durable Session event is missing aggregate sequence")
	}
	if err := ProjectPrompted(tx, ProjectPromptedInput{
		ID: data.MessageID, SessionID: data.SessionID, Prompt: data.Prompt, Delivery: data.Delivery,
		TimeCreated: int64(data.Timestamp), PromotedSeq: int64(durable.Seq),
	}); err != nil {
		return err
	}
	return runProjector(event, tx)
}

func projectPromptAdmitted(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventPromptData](event.Data)
	if err != nil {
		return err
	}
	durable := event.Durable
	if durable == nil {
		return errors.New("Durable Session event is missing aggregate sequence")
	}
	return ProjectAdmitted(tx, ProjectAdmittedInput{
		AdmittedSeq: int64(durable.Seq), ID: data.MessageID, SessionID: data.SessionID,
		Prompt: data.Prompt, Delivery: data.Delivery, TimeCreated: int64(data.Timestamp),
	})
}

func projectRevertStaged(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventRevertStagedData](event.Data)
	if err != nil {
		return err
	}
	revert, err := jsonOrNull(data.Revert)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		"UPDATE `session` SET `revert` = ?, `time_updated` = ? WHERE `id` = ?",
		revert, int64(data.Timestamp), string(data.SessionID),
	)
	return err
}

func projectRevertCleared(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventRevertClearedData](event.Data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		"UPDATE `session` SET `revert` = NULL, `time_updated` = ? WHERE `id` = ?",
		int64(data.Timestamp), string(data.SessionID),
	)
	return err
}

func projectRevertCommitted(event schema.EventPayload, tx DB) error {
	data, err := decodeEventData[schema.SessionEventRevertCommittedData](event.Data)
	if err != nil {
		return err
	}
	var boundary int64
	err = tx.QueryRow(
		"SELECT `seq` FROM `session_message` WHERE `session_id` = ? AND `id` = ?",
		string(data.SessionID), string(data.MessageID),
	).Scan(&boundary)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("Revert boundary message not found: " + string(data.MessageID))
		}
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM `session_message` WHERE `session_id` = ? AND `seq` > ?",
		string(data.SessionID), boundary,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM `session_input` WHERE `session_id` = ? AND (`admitted_seq` > ? OR `promoted_seq` > ?)",
		string(data.SessionID), boundary, boundary,
	); err != nil {
		return err
	}
	_, err = tx.Exec(
		"UPDATE `session` SET `revert` = NULL, `time_updated` = ? WHERE `id` = ?",
		int64(data.Timestamp), string(data.SessionID),
	)
	return err
}
