package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	agent "github.com/jiaweisky/go-agent-sdk"
	coresession "github.com/jiaweisky/go-agent-sdk/core/session"
)

// apiError is the uniform JSON error envelope returned by every failure.
type apiError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	SessionID string `json:"sessionID,omitempty"`
	RequestID string `json:"requestID,omitempty"`
	Field     string `json:"field,omitempty"`
	Ref       string `json:"ref,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeData writes a success response wrapped as {"data": ...}.
func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: code, Message: message})
}

// writeSDKError maps an SDK error to an HTTP status. Internal causes are logged
// (with a short ref echoed to the client) and never leaked.
func (s *Server) writeSDKError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrSessionNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, agent.ErrUnknownAgent):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, agent.ErrSessionBusy):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, coresession.ErrStopped):
		writeError(w, http.StatusServiceUnavailable, "unavailable", err.Error())
	default:
		ref := newErrorRef()
		if s.onError != nil {
			s.onError(fmt.Errorf("ref=%s: %w", ref, err))
		}
		writeJSON(w, http.StatusInternalServerError, apiError{
			Error: "internal", Message: "unexpected server error. Check server logs for details.", Ref: ref,
		})
	}
}

// newErrorRef returns a short opaque id correlating a redacted 500 with its log.
func newErrorRef() string {
	return "err_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// decodeJSON reads a request body into v, returning false (and a 400) on error.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}
