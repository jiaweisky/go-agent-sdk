package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
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
	order := query.Get("order")
	search := strings.ToLower(query.Get("search"))
	limit := parseInt(query.Get("limit"), defaultSessionLimit)
	if limit <= 0 || limit > maxSessionLimit {
		limit = defaultSessionLimit
	}
	sessions = selectSessions(sessions, search, order, limit)
	out := make([]sessionInfoDTO, 0, len(sessions))
	for _, item := range sessions {
		out = append(out, fromRootSessionInfo(item))
	}
	writeData(w, http.StatusOK, out)
}

// selectSessions filters by a case-insensitive title substring, orders by
// timeUpdated (desc unless order is "asc") and truncates to limit.
func selectSessions(sessions []agent.SessionInfo, search, order string, limit int) []agent.SessionInfo {
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
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
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
		s.enqueue(id, agent.MessageOptions{Prompt: body.Prompt, Files: body.Files})
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
	idle, err := s.client.WaitSession(r.Context(), r.PathValue("sessionID"), timeout)
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"idle": idle})
}

func (s *Server) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	s.writeSessionMessages(w, r, s.client.SessionHistory)
}

func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	s.writeSessionMessages(w, r, s.client.SessionMessages)
}

// writeSessionMessages verifies the session exists, reads its messages with
// read (History for the full transcript, Messages for the active context) and
// writes them.
func (s *Server) writeSessionMessages(
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
	writeData(w, http.StatusOK, messages)
}

func (s *Server) handleSessionRevertStage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MessageID string `json:"messageID"`
	}
	if !decodeJSON(w, r, &body) {
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
