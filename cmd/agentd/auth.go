package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerToken extracts a bearer token from the Authorization header. It returns
// "" when the header is absent or uses another scheme.
func bearerToken(req *http.Request) string {
	value := req.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

// constEq compares two secrets in constant time.
func constEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
