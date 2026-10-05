package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	coresession "github.com/jiaweisky/go-agent-sdk/core/session"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

// Turn metadata keys used to persist an A2A task and the local session it
// mirrors. The version key makes optimistic concurrency control survive a
// restart.
const (
	metaKeyTask    = "a2a.task"
	metaKeyContext = "a2a.context_id"
	metaKeyVersion = "a2a.version"
	metaKeySession = "sdk_session_id"
	metaKeyAgent   = "sdk_agent_id"
	// metaKeyPendingRequest travels in the input-required status event metadata,
	// not the turn metadata; the executor reads it in pendingRequestFrom.
	metaKeyPendingRequest = "sdk_pending_request_id"
)

// turnTaskStore is a taskstore.Store persisted on the core `turn` table. The
// A2A task, its context and the local session it mirrors are serialized into
// the turn's metadata, which keeps the task store stateless and its versions
// durable across restarts.
type turnTaskStore struct {
	turns *coresession.TurnStore
}

var _ taskstore.Store = (*turnTaskStore)(nil)

// NewTurnTaskStore returns a taskstore.Store backed by the core turn table.
func NewTurnTaskStore(turns *coresession.TurnStore) taskstore.Store {
	return &turnTaskStore{turns: turns}
}

func (s *turnTaskStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	if task == nil || task.ID == "" || task.ContextID == "" {
		return taskstore.TaskVersionMissing, errors.New("task id and context id are required")
	}
	sessionID := taskMetaString(task, metaKeySession)
	if sessionID == "" {
		return taskstore.TaskVersionMissing, errors.New("task metadata is missing " + metaKeySession)
	}
	existing, err := s.turns.Get(schema.TurnID(task.ID))
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	if existing != nil {
		return taskstore.TaskVersionMissing, taskstore.ErrTaskAlreadyExists
	}
	const version taskstore.TaskVersion = 1
	now := nowMillis()
	anchor, _ := s.turns.LatestSeq(schema.SessionID(sessionID))
	info := schema.TurnInfo{
		ID:          schema.TurnID(task.ID),
		SessionID:   schema.SessionID(sessionID),
		State:       turnStateFromA2A(task.Status.State),
		AnchorSeq:   &anchor,
		Metadata:    turnMetadata(task, sessionID, taskMetaString(task, metaKeyAgent), version),
		TimeCreated: now,
		TimeUpdated: now,
	}
	if err := s.turns.Create(info); err != nil {
		return taskstore.TaskVersionMissing, err
	}
	return version, nil
}

func (s *turnTaskStore) Update(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	if req == nil || req.Task == nil {
		return taskstore.TaskVersionMissing, a2a.ErrTaskNotFound
	}
	existing, err := s.turns.Get(schema.TurnID(req.Task.ID))
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	if existing == nil {
		return taskstore.TaskVersionMissing, a2a.ErrTaskNotFound
	}

	current := decodeVersion(existing.Metadata)
	if req.PrevVersion != taskstore.TaskVersionMissing && current != taskstore.TaskVersionMissing && current != req.PrevVersion {
		return taskstore.TaskVersionMissing, taskstore.ErrConcurrentModification
	}
	next := current + 1

	sessionID := taskMetaString(req.Task, metaKeySession)
	if sessionID == "" {
		sessionID = string(existing.SessionID)
	}
	agentID := taskMetaString(req.Task, metaKeyAgent)
	if agentID == "" {
		agentID = metadataString(existing.Metadata, metaKeyAgent)
	}
	existing.State = turnStateFromA2A(req.Task.Status.State)
	existing.Metadata = turnMetadata(req.Task, sessionID, agentID, next)
	existing.PendingRequestID = pendingRequestFrom(req.Event)
	if existing.State.Terminal() {
		end, _ := s.turns.LatestSeq(existing.SessionID)
		existing.EndSeq = &end
	}
	existing.TimeUpdated = nowMillis()
	if err := s.turns.Update(*existing); err != nil {
		return taskstore.TaskVersionMissing, err
	}
	return next, nil
}

func (s *turnTaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	info, err := s.turns.Get(schema.TurnID(taskID))
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, a2a.ErrTaskNotFound
	}
	task, err := decodeTask(info.Metadata)
	if err != nil {
		return nil, err
	}
	return &taskstore.StoredTask{Task: task, Version: decodeVersion(info.Metadata)}, nil
}

// taskEntry pairs a decoded task with its turn's last-updated time, for listing.
type taskEntry struct {
	task    *a2a.Task
	updated int64
}

func (s *turnTaskStore) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	infos, err := s.turns.ListAll()
	if err != nil {
		return nil, err
	}
	pageSize := 50
	if req != nil && req.PageSize > 0 {
		pageSize = req.PageSize
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100 inclusive, got %d: %w", pageSize, a2a.ErrInvalidRequest)
	}

	entries := make([]taskEntry, 0, len(infos))
	for _, info := range infos {
		task, err := decodeTask(info.Metadata)
		if err != nil {
			continue
		}
		if req != nil && req.ContextID != "" && task.ContextID != req.ContextID {
			continue
		}
		if req != nil && req.Status != a2a.TaskStateUnspecified && task.Status.State != req.Status {
			continue
		}
		if req != nil && req.StatusTimestampAfter != nil && task.Status.Timestamp != nil && task.Status.Timestamp.Before(*req.StatusTimestampAfter) {
			continue
		}
		entries = append(entries, taskEntry{task: task, updated: info.TimeUpdated})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].updated != entries[j].updated {
			return entries[i].updated > entries[j].updated
		}
		return string(entries[i].task.ID) > string(entries[j].task.ID)
	})
	total := len(entries)

	start := 0
	if req != nil && req.PageToken != "" {
		cursorUpdated, cursorID, err := decodeTaskPageToken(req.PageToken)
		if err != nil {
			return nil, err
		}
		for start < len(entries) && !afterCursor(entries[start], cursorUpdated, cursorID) {
			start++
		}
	}
	page := entries[start:]
	next := ""
	if pageSize < len(page) {
		last := page[pageSize-1]
		next = encodeTaskPageToken(last.updated, string(last.task.ID))
		page = page[:pageSize]
	}
	tasks := make([]*a2a.Task, 0, len(page))
	for _, item := range page {
		tasks = append(tasks, trimTask(item.task, req))
	}
	// PageSize reports the number returned, not requested (matching a2a-go).
	returned := pageSize
	if returned > len(page) {
		returned = len(page)
	}
	return &a2a.ListTasksResponse{Tasks: tasks, TotalSize: total, PageSize: returned, NextPageToken: next}, nil
}

// afterCursor reports whether an entry follows the (updated, id) cursor in the
// newest-first ordering.
func afterCursor(e taskEntry, updated int64, id string) bool {
	if e.updated != updated {
		return e.updated < updated
	}
	return string(e.task.ID) < id
}

// trimTask applies HistoryLength and IncludeArtifacts to a listed task.
func trimTask(task *a2a.Task, req *a2a.ListTasksRequest) *a2a.Task {
	historyLength := 100
	if req != nil && req.HistoryLength != nil {
		historyLength = *req.HistoryLength
	}
	if historyLength <= 0 {
		task.History = []*a2a.Message{}
	} else if len(task.History) > historyLength {
		task.History = task.History[len(task.History)-historyLength:]
	}
	if req == nil || !req.IncludeArtifacts {
		task.Artifacts = nil
	}
	return task
}

func encodeTaskPageToken(updated int64, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(strconv.FormatInt(updated, 10) + "_" + id))
}

func decodeTaskPageToken(token string) (int64, string, error) {
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return 0, "", a2a.ErrParseError
	}
	updatedPart, id, ok := strings.Cut(string(decoded), "_")
	if !ok {
		return 0, "", a2a.ErrParseError
	}
	updated, err := strconv.ParseInt(updatedPart, 10, 64)
	if err != nil {
		return 0, "", a2a.ErrParseError
	}
	return updated, id, nil
}

// SessionForContext returns the mirrored local session for an A2A context owned
// by agentID, if one exists. Scoping by agent keeps two exposed agents from
// ever sharing a session on a colliding context id.
func (s *turnTaskStore) SessionForContext(agentID, contextID string) (string, bool) {
	if contextID == "" {
		return "", false
	}
	infos, err := s.turns.FindByMetadata(metaKeyContext, contextID)
	if err != nil {
		return "", false
	}
	for _, info := range infos {
		if metadataString(info.Metadata, metaKeyAgent) != agentID {
			continue
		}
		if sessionID := metadataString(info.Metadata, metaKeySession); sessionID != "" {
			return sessionID, true
		}
	}
	return "", false
}

// pendingRequestFrom extracts the request id a turn is parked on from an
// input-required status event. The id travels in event metadata so a2a-go's
// single processor goroutine stays the only writer of the turn row; the
// executor never writes it out of band.
func pendingRequestFrom(event a2a.Event) *string {
	status, ok := event.(*a2a.TaskStatusUpdateEvent)
	if !ok {
		return nil
	}
	switch status.Status.State {
	case a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired:
	default:
		return nil
	}
	requestID, _ := status.Metadata[metaKeyPendingRequest].(string)
	if requestID == "" {
		return nil
	}
	return &requestID
}

// turnMetadata serializes a task and its bindings into the turn metadata map.
func turnMetadata(task *a2a.Task, sessionID, agentID string, version taskstore.TaskVersion) map[string]any {
	encoded, _ := json.Marshal(task)
	return map[string]any{
		metaKeyTask:          string(encoded),
		metaKeySession:       sessionID,
		metaKeyAgent:         agentID,
		metaKeyContext:       task.ContextID,
		metaKeyVersion:       version,
		schema.TurnMetaOwner: schema.TurnOwnerA2A,
	}
}

// decodeTask reads the task back out of turn metadata.
func decodeTask(metadata map[string]any) (*a2a.Task, error) {
	raw := metadataString(metadata, metaKeyTask)
	if raw == "" {
		return nil, errors.New("turn metadata is missing " + metaKeyTask)
	}
	var task a2a.Task
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// decodeVersion reads the task version; a missing version is TaskVersionMissing.
func decodeVersion(metadata map[string]any) taskstore.TaskVersion {
	switch value := metadata[metaKeyVersion].(type) {
	case float64:
		return taskstore.TaskVersion(value)
	case int:
		return taskstore.TaskVersion(value)
	case int64:
		return taskstore.TaskVersion(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return taskstore.TaskVersion(parsed)
		}
	}
	return taskstore.TaskVersionMissing
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func taskMetaString(task *a2a.Task, key string) string {
	return metadataString(task.Metadata, key)
}

// turnStateFromA2A maps an A2A task state onto the core turn state.
func turnStateFromA2A(state a2a.TaskState) schema.TurnState {
	switch state {
	case a2a.TaskStateCompleted:
		return schema.TurnStateCompleted
	case a2a.TaskStateFailed:
		return schema.TurnStateFailed
	case a2a.TaskStateCanceled:
		return schema.TurnStateCanceled
	case a2a.TaskStateRejected:
		return schema.TurnStateRejected
	case a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired:
		return schema.TurnStateAwaitingInput
	default:
		return schema.TurnStateRunning
	}
}
