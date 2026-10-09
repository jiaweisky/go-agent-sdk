package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				writeError(w, http.StatusInternalServerError, "internal", "unexpected server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withJSONErrors normalises the router's plain-text 404/405 responses into the
// error envelope, leaving handler-written JSON responses untouched.
func (s *Server) withJSONErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&jsonErrorWriter{ResponseWriter: w, status: http.StatusOK}, r)
	})
}

type jsonErrorWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	intercept   bool
}

func (w *jsonErrorWriter) WriteHeader(status int) {
	if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
		w.Header().Get("Content-Type") != "application/json" {
		w.status = status
		w.intercept = true
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

// Flush lets handlers that assert http.Flusher (SSE) keep working through the
// wrapper.
func (w *jsonErrorWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *jsonErrorWriter) Write(b []byte) (int, error) {
	if w.intercept {
		code := "not_found"
		if w.status == http.StatusMethodNotAllowed {
			code = "method_not_allowed"
		}
		w.Header().Set("Content-Type", "application/json")
		w.ResponseWriter.WriteHeader(w.status)
		_ = json.NewEncoder(w.ResponseWriter).Encode(apiError{Error: code, Message: http.StatusText(w.status)})
		w.intercept = false
		return len(b), nil
	}
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(w.status)
	}
	return w.ResponseWriter.Write(b)
}
