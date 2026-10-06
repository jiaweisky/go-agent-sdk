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
		writeSDKError(w, err)
		return
	}
	order := r.URL.Query().Get("order")
	search := strings.ToLower(r.URL.Query().Get("search"))
	limit := parseInt(r.URL.Query().Get("limit"), defaultSessionLimit)
	if limit <= 0 || limit > maxSessionLimit {
		limit = defaultSessionLimit
	}
	if search != "" {
		filtered := sessions[:0]
		for _, item := range sessions {
			if strings.Contains(strings.ToLower(item.Title), search) {
				filtered = append(filtered, item)
			}
		}
		sessions = filtered
	}
	if order == "asc" {
		sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].TimeUpdated < sessions[j].TimeUpdated })
	} else {
		sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].TimeUpdated > sessions[j].TimeUpdated })
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	out := make([]sessionInfoDTO, 0, len(sessions))
	for _, item := range sessions {
		out = append(out, fromRootSessionInfo(item))
	}
	writeData(w, http.StatusOK, out)
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
	if s.client.Agents().Get(schema.AgentID(agentID)) == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown agent: "+agentID)
		return
	}
	if !s.allowed(agentID) {
		writeError(w, http.StatusForbidden, "forbidden", "agent not allowed: "+agentID)
		return
	}
	handle, err := s.client.CreateSession(r.Context(), &agent.SessionConfig{
		Agent: agentID, Model: body.Model, Title: body.Title, Permissions: s.inbound,
	})
	if err != nil {
		writeSDKError(w, err)
		return
	}
	id := handle.ID()
	info, err := s.client.GetSession(r.Context(), id)
	// The session row persists; drop the handle so an unused session keeps no
	// event subscription.
	_ = handle.Close()
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, fromSchemaSessionInfo(info))
}

func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetSession(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, fromSchemaSessionInfo(info))
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		writeSDKError(w, err)
		return
	}
	_ = s.withHandle(r.Context(), id, func(h *agent.Session) error {
		h.Abort()
		return nil
	})
	// A parked permission/question ask cannot be released by Abort; reject them
	// so the worker can drain before we delete the rows.
	s.rejectPending(id)
	deadline := time.Now().Add(5 * time.Second)
	for {
		e := s.entry(id)
		e.mu.Lock()
		busy := e.worker != nil
		e.mu.Unlock()
		if !busy || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.evict(id)
	if err := s.client.DeleteSession(r.Context(), id); err != nil {
		writeSDKError(w, err)
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
	if s.client.Agents().Get(schema.AgentID(body.Agent)) == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown agent: "+body.Agent)
		return
	}
	if !s.allowed(body.Agent) {
		writeError(w, http.StatusForbidden, "forbidden", "agent not allowed: "+body.Agent)
		return
	}
	if err := s.withHandle(r.Context(), r.PathValue("sessionID"), func(h *agent.Session) error {
		return h.SetAgent(body.Agent)
	}); err != nil {
		writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	if err := s.withHandle(r.Context(), r.PathValue("sessionID"), func(h *agent.Session) error {
		return h.SetModel(ref)
	}); err != nil {
		writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessionSetTitle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.withHandle(r.Context(), r.PathValue("sessionID"), func(h *agent.Session) error {
		return h.SetTitle(body.Title)
	}); err != nil {
		writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		writeSDKError(w, err)
		return
	}
	if body.Delivery == "steer" {
		if err := s.withHandle(r.Context(), id, func(h *agent.Session) error {
			return h.Steer(body.Prompt)
		}); err != nil {
			writeSDKError(w, err)
			return
		}
	} else {
		s.enqueue(id, agent.MessageOptions{Prompt: body.Prompt, Files: body.Files})
	}
	writeData(w, http.StatusAccepted, map[string]any{"queued": true})
}

func (s *Server) handleSessionInterrupt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		writeSDKError(w, err)
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
		writeSDKError(w, err)
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
			// A non-positive timeout must not mean "wait forever" (D3).
			writeError(w, http.StatusBadRequest, "bad_request", "timeout must be positive")
			return
		}
		timeout = parsed
	}
	idle, err := s.client.WaitSession(r.Context(), r.PathValue("sessionID"), timeout)
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"idle": idle})
}

func (s *Server) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	if _, err := s.client.GetSession(r.Context(), r.PathValue("sessionID")); err != nil {
		writeSDKError(w, err)
		return
	}
	messages, err := s.client.SessionHistory(r.PathValue("sessionID"))
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, messages)
}

func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	if _, err := s.client.GetSession(r.Context(), r.PathValue("sessionID")); err != nil {
		writeSDKError(w, err)
		return
	}
	messages, err := s.client.SessionMessages(r.PathValue("sessionID"))
	if err != nil {
		writeSDKError(w, err)
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
		writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, state)
}

func (s *Server) handleSessionRevertGet(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetSession(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, info.Revert)
}

func (s *Server) handleSessionRevertClear(w http.ResponseWriter, r *http.Request) {
	if err := s.withHandle(r.Context(), r.PathValue("sessionID"), func(h *agent.Session) error {
		return h.RevertClear()
	}); err != nil {
		writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessionRevertCommit(w http.ResponseWriter, r *http.Request) {
	if err := s.withHandle(r.Context(), r.PathValue("sessionID"), func(h *agent.Session) error {
		return h.RevertCommit()
	}); err != nil {
		writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
