package server

import (
	"net/http"

	agent "github.com/jiaweisky/go-agent-sdk"
)

func (s *Server) handlePermissionList(w http.ResponseWriter, r *http.Request) {
	requests := s.client.Permission().List()
	writeData(w, http.StatusOK, permissionDTOs(requests))
}

func (s *Server) handleSessionPermissionList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return
	}
	writeData(w, http.StatusOK, permissionDTOs(s.client.Permission().ForSession(id)))
}

// pendingPermission returns the pending permission request owned by the path
// session, writing 404 and returning nil otherwise.
func (s *Server) pendingPermission(w http.ResponseWriter, r *http.Request) *agent.PermissionRequest {
	id := r.PathValue("sessionID")
	if _, err := s.client.GetSession(r.Context(), id); err != nil {
		s.writeSDKError(w, err)
		return nil
	}
	req := s.client.Permission().Get(r.PathValue("requestID"))
	if req == nil || req.SessionID != id {
		writeError(w, http.StatusNotFound, "not_found", "permission request not found")
		return nil
	}
	return req
}

func (s *Server) handleSessionPermissionGet(w http.ResponseWriter, r *http.Request) {
	req := s.pendingPermission(w, r)
	if req == nil {
		return
	}
	writeData(w, http.StatusOK, fromPermissionRequest(*req))
}

func (s *Server) handleSessionPermissionReply(w http.ResponseWriter, r *http.Request) {
	if s.pendingPermission(w, r) == nil {
		return
	}
	var body struct {
		Reply   string `json:"reply"`
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	switch body.Reply {
	case "once", "always", "reject":
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "reply must be once, always or reject")
		return
	}
	if err := s.client.Permission().Reply(agent.PermissionReplyInput{
		RequestID: r.PathValue("requestID"), Reply: agent.PermissionReply(body.Reply), Message: body.Message,
	}); err != nil {
		s.writeSDKError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func permissionDTOs(requests []agent.PermissionRequest) []permissionRequestDTO {
	out := make([]permissionRequestDTO, 0, len(requests))
	for _, req := range requests {
		out = append(out, fromPermissionRequest(req))
	}
	return out
}
