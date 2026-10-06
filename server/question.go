package server

import (
	"net/http"

	agent "github.com/jiaweisky/go-agent-sdk"
	"github.com/jiaweisky/go-agent-sdk/schema"
)

func (s *Server) handleQuestionList(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, questionDTOs(s.client.Question().List()))
}

func (s *Server) handleSessionQuestionList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return
	}
	var requests []agent.QuestionRequest
	for _, req := range s.client.Question().List() {
		if req.SessionID == id {
			requests = append(requests, req)
		}
	}
	writeData(w, http.StatusOK, questionDTOs(requests))
}

func (s *Server) handleSessionQuestionGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return
	}
	req := s.client.Question().Get(r.PathValue("requestID"))
	if req == nil || req.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "question request not found")
		return
	}
	writeData(w, http.StatusOK, fromQuestionRequest(*req))
}

func (s *Server) handleSessionQuestionReply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return
	}
	requestID := r.PathValue("requestID")
	req := s.client.Question().Get(requestID)
	if req == nil || req.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "question request not found")
		return
	}
	var body struct {
		Answers []schema.QuestionAnswer `json:"answers"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.client.Question().Reply(requestID, body.Answers); err != nil {
		s.writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessionQuestionReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return
	}
	requestID := r.PathValue("requestID")
	req := s.client.Question().Get(requestID)
	if req == nil || req.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "question request not found")
		return
	}
	if err := s.client.Question().Reject(requestID); err != nil {
		s.writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func questionDTOs(requests []agent.QuestionRequest) []questionRequestDTO {
	out := make([]questionRequestDTO, 0, len(requests))
	for _, req := range requests {
		out = append(out, fromQuestionRequest(req))
	}
	return out
}
