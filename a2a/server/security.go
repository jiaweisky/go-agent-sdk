package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Security describes the authentication the server requires on its JSON-RPC
// endpoints. Cards remain public so clients can discover the scheme.
type Security struct {
	// Bearer is the expected token; a request must send
	// "Authorization: Bearer <token>".
	Bearer string
	// SchemeName is the card's securitySchemes key. Empty defaults to "bearer".
	SchemeName string
}

// securitySchemeName returns the configured scheme key or its default.
func (s *Security) securitySchemeName() string {
	if s.SchemeName != "" {
		return s.SchemeName
	}
	return "bearer"
}

// secure wraps a handler with the configured authentication. It is a no-op when
// Security is nil.
func (s *Server) secure(next http.Handler) http.Handler {
	if s.opts.Security == nil {
		return next
	}
	token := s.opts.Security.Bearer
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerToken extracts the token from an Authorization header value.
func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
