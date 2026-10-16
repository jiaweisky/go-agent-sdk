package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	coresession "github.com/jiaweisky/go-agent-sdk/internal/core/session"
	agent "github.com/jiaweisky/go-agent-sdk/pkg/agent"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
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
	var revertNotFound *coresession.RevertMessageNotFoundError
	switch {
	case errors.Is(err, agent.ErrSessionNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, agent.ErrUnknownAgent):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, agent.ErrSessionBusy):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, coresession.ErrStopped), errors.Is(err, core.ErrSnapshotUnavailable):
		writeError(w, http.StatusServiceUnavailable, "unavailable", err.Error())
	case errors.As(err, &revertNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
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
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("request body exceeds the %d-byte limit", tooLarge.Limit))
		return false
	}
	writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
	return false
}

// withMaxBody caps every request body, so an oversized payload is rejected at
// read time rather than buffered whole.
func withMaxBody(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
