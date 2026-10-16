package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMaxBodyRejectsOversized: a JSON-RPC request body over MaxBodyBytes is
// rejected with 413 up front, while a within-limit body passes through.
func TestMaxBodyRejectsOversized(t *testing.T) {
	srv := &Server{opts: Options{MaxBodyBytes: 100}}
	h := srv.maxBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agents/a/", strings.NewReader(strings.Repeat("x", 200))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agents/a/", strings.NewReader("ok")))
	if rec.Code != http.StatusOK {
		t.Fatalf("small body = %d, want 200", rec.Code)
	}
}
