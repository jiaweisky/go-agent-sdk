package server

import (
	"net/http"
	"strings"
)

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && s.allowedOrigin(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowedOrigin allows the configured origins plus same-origin/loopback by
// default (matching opencode).
func (s *Server) allowedOrigin(origin string) bool {
	for _, o := range s.opts.CORS {
		if o == origin {
			return true
		}
	}
	return origin == "http://localhost" || strings.HasPrefix(origin, "http://localhost:") ||
		origin == "http://127.0.0.1" || strings.HasPrefix(origin, "http://127.0.0.1:")
}
