package server

import (
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const (
	defaultSessionLimit = 50
	maxSessionLimit     = 200
	defaultWaitTimeout  = 30 * time.Second
)

func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.client.ListSessions(r.Context())
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	query := r.URL.Query()
	sessions = filterSortSessions(sessions, strings.ToLower(query.Get("search")), query.Get("order"))
	limit := clampLimit(parseInt(query.Get("limit"), defaultSessionLimit), defaultSessionLimit, maxSessionLimit)
	page := paginate(sessions, func(item agent.SessionInfo) string { return item.ID }, decodeCursor(query.Get("cursor")), limit)
	out := make([]sessionInfoDTO, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, fromRootSessionInfo(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":   out,
		"cursor": map[string]any{"previous": nullable(page.Previous), "next": nullable(page.Next)},
	})
}

// filterSortSessions filters by a case-insensitive title substring and orders by
// timeUpdated (desc unless order is "asc").
func filterSortSessions(sessions []agent.SessionInfo, search, order string) []agent.SessionInfo {
	if search != "" {
		filtered := sessions[:0]
		for _, item := range sessions {
			if strings.Contains(strings.ToLower(item.Title), search) {
				filtered = append(filtered, item)
			}
		}
		sessions = filtered
	}
	ascending := order == "asc"
	sort.SliceStable(sessions, func(i, j int) bool {
		if ascending {
			return sessions[i].TimeUpdated < sessions[j].TimeUpdated
		}
		return sessions[i].TimeUpdated > sessions[j].TimeUpdated
	})
	return sessions
}

type createSessionRequest struct {
	Agent string `json:"agent"`
	Model string `json:"model"`
	Title string `json:"title"`
}

func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var body createSessionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	agentID := body.Agent
	if agentID == "" {
		agentID = s.defaultAgent()
	}
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "agent is required")
		return
	}
	if !s.requireAgent(w, agentID) {
		return
	}
	handle, err := s.client.CreateSession(r.Context(), &agent.SessionConfig{
		Agent: agentID, Model: body.Model, Title: body.Title, Permissions: s.inbound,
	})
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	id := handle.ID()
	info, infoErr := s.client.GetSession(r.Context(), id)
	// The session row persists; drop the handle so an unused session keeps no
	// event subscription.
	_ = handle.Close()
	if infoErr != nil {
		s.writeSDKError(w, infoErr)
		return
	}
	writeData(w, http.StatusOK, fromSchemaSessionInfo(info))
}

// handleSessionActive lists sessions currently running a turn.
func (s *Server) handleSessionActive(w http.ResponseWriter, r *http.Request) {
	data := map[string]map[string]string{}
	for _, id := range s.client.ActiveSessions() {
		data[id] = map[string]string{"type": "running"}
	}
	writeData(w, http.StatusOK, data)
}

const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 100
)

// handleSessionHistory returns a finite page of a session's durable events.
func (s *Server) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	query := r.URL.Query()
	after := -1 // from the beginning unless a sequence is given
	if raw := query.Get("after"); raw != "" {
		after = parseInt(raw, 0)
	}
	limit := parseInt(query.Get("limit"), defaultHistoryLimit)
	if limit <= 0 || limit > maxHistoryLimit {
		limit = defaultHistoryLimit
	}
	events, err := s.client.SessionEvents(id, after)
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	out := make([]wireEvent, 0, len(events))
	for _, ev := range events {
		out = append(out, toWireEvent(ev))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "hasMore": hasMore})
}

func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetSession(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, fromSchemaSessionInfo(info))
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	_ = s.withHandle(r.Context(), id, func(h *agent.Session) error {
		h.Abort()
		return nil
	})
	// A parked permission/question ask cannot be released by Abort; reject them
	// so the worker can drain before we delete the rows.
	s.rejectPending(id)
	s.drainWorker(id, 5*time.Second)
	s.evict(id)
	if err := s.client.DeleteSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessionSetAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !s.requireAgent(w, body.Agent) {
		return
	}
	s.mutate(w, r, func(h *agent.Session) error { return h.SetAgent(body.Agent) })
}

func (s *Server) handleSessionSetModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string  `json:"id"`
		ProviderID string  `json:"providerID"`
		Variant    *string `json:"variant"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "model id is required")
		return
	}
	ref := schema.ModelRef{ID: schema.ModelID(body.ID), ProviderID: schema.ProviderID(body.ProviderID)}
	if body.Variant != nil {
		variant := schema.ModelVariantID(*body.Variant)
		ref.Variant = &variant
	}
	s.mutate(w, r, func(h *agent.Session) error { return h.SetModel(ref) })
}

func (s *Server) handleSessionSetTitle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	s.mutate(w, r, func(h *agent.Session) error { return h.SetTitle(body.Title) })
}

type promptRequest struct {
	Prompt   string                        `json:"prompt"`
	ID       string                        `json:"id"`
	Delivery string                        `json:"delivery"`
	Files    []schema.PromptFileAttachment `json:"files"`
}

func (s *Server) handleSessionPrompt(w http.ResponseWriter, r *http.Request) {
	var body promptRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Prompt == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "prompt is required")
		return
	}
	if body.Delivery != "" && body.Delivery != "queue" && body.Delivery != "steer" {
		writeError(w, http.StatusBadRequest, "bad_request", "delivery must be queue or steer")
		return
	}
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	if body.Delivery == "steer" {
		if err := s.withHandle(r.Context(), id, func(h *agent.Session) error {
			return h.Steer(body.Prompt)
		}); err != nil {
			s.writeSDKError(w, err)
			return
		}
	} else {
		s.enqueue(id, agent.MessageOptions{Prompt: body.Prompt, ID: body.ID, Files: body.Files})
	}
	writeData(w, http.StatusAccepted, map[string]any{"queued": true})
}

func (s *Server) handleSessionInterrupt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	_ = s.withHandle(r.Context(), id, func(h *agent.Session) error {
		h.Abort()
		return nil
	})
	// Abort cannot release a parked permission ask; reject pending interactions.
	s.rejectPending(id)
	w.WriteHeader(http.StatusNoContent)
}

// rejectPending declines every pending permission and rejects every pending
// question for a session, so a parked turn can drain.
func (s *Server) rejectPending(sessionID string) {
	for _, req := range s.client.Permission().ForSession(sessionID) {
		_ = s.client.Permission().Reply(agent.PermissionReplyInput{
			RequestID: req.ID, Reply: agent.PermissionReplyReject,
		})
	}
	for _, req := range s.client.Question().List() {
		if req.SessionID == sessionID {
			_ = s.client.Question().Reject(req.ID)
		}
	}
}

func (s *Server) handleSessionCompact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	var compacted bool
	err := s.withHandle(r.Context(), id, func(h *agent.Session) error {
		var compactErr error
		compacted, compactErr = h.Compact(r.Context())
		return compactErr
	})
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"compacted": compacted})
}

func (s *Server) handleSessionWait(w http.ResponseWriter, r *http.Request) {
	timeout := defaultWaitTimeout
	if raw := r.URL.Query().Get("timeout"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid timeout")
			return
		}
		if parsed <= 0 {
			// A non-positive timeout must not turn into an unbounded wait.
			writeError(w, http.StatusBadRequest, "bad_request", "timeout must be positive")
			return
		}
		timeout = parsed
	}
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	// WaitSession only sees the SDK coordinator, which stays idle until the
	// prompt worker calls Session.Send. Wait for the worker to drain first, or a
	// prompt accepted moments ago would report idle before its turn started.
	deadline := time.Now().Add(timeout)
	if !s.waitForWorker(id, deadline) {
		writeIdle(w, false)
		return
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		writeIdle(w, false)
		return
	}
	idle, err := s.client.WaitSession(r.Context(), id, remaining)
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeIdle(w, idle)
}

// waitWorkerPollInterval is how often /wait checks the session worker.
const waitWorkerPollInterval = 20 * time.Millisecond

// waitForWorker blocks until the session's prompt worker drains or the deadline
// passes, reporting whether it drained.
func (s *Server) waitForWorker(id string, deadline time.Time) bool {
	for s.workerBusy(id) {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(waitWorkerPollInterval)
	}
	return true
}

// workerBusy reports whether a session has a prompt worker draining its queue.
func (s *Server) workerBusy(id string) bool {
	e := s.entry(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.worker != nil
}

// writeIdle writes the /wait result.
func writeIdle(w http.ResponseWriter, idle bool) {
	writeData(w, http.StatusOK, map[string]any{"idle": idle})
}

// handleSessionMessage serves the full transcript as a paged page.
func (s *Server) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	s.writeSessionMessagesPage(w, r, s.client.SessionHistory)
}

// handleSessionContext serves the whole active context (no paging).
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	messages, err := s.client.SessionMessages(id)
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, messages)
}

const (
	defaultMessageLimit = 50
	maxMessageLimit     = 200
)

// writeSessionMessagesPage reads messages with read and returns one ordered
// page with cursors. order is "asc" (default, chronological) or "desc".
func (s *Server) writeSessionMessagesPage(
	w http.ResponseWriter,
	r *http.Request,
	read func(string) ([]schema.SessionMessage, error),
) {
	id := r.PathValue("sessionID")
	if !s.requireSession(w, r) {
		return
	}
	messages, err := read(id)
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	query := r.URL.Query()
	if query.Get("order") == "desc" {
		slices.Reverse(messages)
	}
	limit := clampLimit(parseInt(query.Get("limit"), defaultMessageLimit), defaultMessageLimit, maxMessageLimit)
	page := paginate(messages,
		func(m schema.SessionMessage) string { return string(m.SessionMessageID()) },
		decodeCursor(query.Get("cursor")), limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"data":   page.Items,
		"cursor": map[string]any{"previous": nullable(page.Previous), "next": nullable(page.Next)},
	})
}

func (s *Server) handleSessionRevertStage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MessageID string `json:"messageID"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.MessageID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "messageID is required")
		return
	}
	var state *schema.RevertState
	err := s.withHandle(r.Context(), r.PathValue("sessionID"), func(h *agent.Session) error {
		var stageErr error
		state, stageErr = h.RevertStage(body.MessageID)
		return stageErr
	})
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, state)
}

func (s *Server) handleSessionRevertGet(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetSession(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, info.Revert)
}

func (s *Server) handleSessionRevertClear(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(h *agent.Session) error { return h.RevertClear() })
}

func (s *Server) handleSessionRevertCommit(w http.ResponseWriter, r *http.Request) {
	s.mutate(w, r, func(h *agent.Session) error { return h.RevertCommit() })
}

// mutate runs fn against the session's handle and writes 204 on success, or
// maps the error. It is the shared shape for the session state changes that
// return no content.
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, fn func(*agent.Session) error) {
	if err := s.withHandle(r.Context(), r.PathValue("sessionID"), fn); err != nil {
		s.writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// drainWorker waits, up to timeout, for a session's worker to finish.
func (s *Server) drainWorker(id string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		e := s.entry(id)
		e.mu.Lock()
		busy := e.worker != nil
		e.mu.Unlock()
		if !busy || time.Now().After(deadline) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *Server) evict(id string) {
	s.mu.Lock()
	e := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if e != nil {
		e.mu.Lock()
		if e.handle != nil {
			_ = e.handle.Close()
			e.handle = nil
		}
		e.mu.Unlock()
	}
}

func parseInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
