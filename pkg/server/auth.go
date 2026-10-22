package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Auth requires a bearer token on every /api route except Options.Public.
type Auth struct {
	Token string
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	if s.opts.Auth == nil {
		return next
	}
	public := make(map[string]bool, len(s.opts.Public))
	for _, p := range s.opts.Public {
		public[p] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || public[r.URL.Path] || s.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", s.authChallenge())
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	})
}

func (s *Server) authChallenge() string {
	return "Bearer"
}

func (s *Server) authorized(r *http.Request) bool {
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if token == "" {
		token = r.URL.Query().Get("auth_token")
	}
	return constEq(token, s.opts.Auth.Token)
}

func constEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
