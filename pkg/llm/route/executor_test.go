package route

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecutorRetriesRetryableStatus(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			// A 500 maps to ProviderInternalReason, which is retryable.
			w.Header().Set("Retry-After-Ms", "0")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, llmErr := NewExecutor(server.Client()).DoWithRetries(req)
	if llmErr != nil {
		t.Fatalf("DoWithRetries: %v", llmErr)
	}
	defer resp.Body.Close()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestExecutorDoesNotRetryInvalidRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, llmErr := NewExecutor(server.Client()).DoWithRetries(req); llmErr == nil {
		t.Fatal("want an error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestExecutorRedactsErrorContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad","api_key":"sk-body-secret"}`))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"?key=sk-query-secret", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer sk-header-secret")

	_, llmErr := NewExecutor(server.Client()).DoWithRetries(req)
	if llmErr == nil {
		t.Fatal("want an error")
	}
	reason := llmErr.Reason
	if strings.Contains(reason.Message, "sk-body-secret") || !strings.Contains(reason.Message, redacted) {
		t.Fatalf("message = %q", reason.Message)
	}
	if reason.Http == nil {
		t.Fatal("http context missing")
	}
	if strings.Contains(reason.Http.Request.URL, "sk-query-secret") {
		t.Fatalf("query secret leaked: %s", reason.Http.Request.URL)
	}
	if reason.Http.Request.Headers["Authorization"] != redacted {
		t.Fatalf("authorization header = %q", reason.Http.Request.Headers["Authorization"])
	}
	if reason.Http.Body == nil || strings.Contains(*reason.Http.Body, "sk-body-secret") {
		t.Fatalf("body = %v", reason.Http.Body)
	}
}

func TestExecutorRecordsRequestAndResponseDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_123")
		w.Header().Set("Retry-After-Ms", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_, llmErr := NewExecutor(server.Client()).DoWithRetries(req)
	if llmErr == nil {
		t.Fatal("want an error")
	}
	reason := llmErr.Reason
	if reason.Http == nil {
		t.Fatal("http context missing")
	}
	if reason.Http.Request.Method != http.MethodPost || reason.Http.Request.URL == "" {
		t.Fatalf("request details = %+v", reason.Http.Request)
	}
	if reason.Http.Response == nil || reason.Http.Response.Status != http.StatusTooManyRequests {
		t.Fatalf("response details = %+v", reason.Http.Response)
	}
	if reason.Http.RequestID == nil || *reason.Http.RequestID != "req_123" {
		t.Fatalf("request id = %v", reason.Http.RequestID)
	}
	if !reason.Retryable() {
		t.Fatal("rate limit must be retryable")
	}
}

func TestTransportErrorKind(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.test/v1/chat?key=secret", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	timeout := transportError(req, &net.DNSError{Err: "i/o timeout", IsTimeout: true})
	if timeout.Reason.TransportKind == nil || *timeout.Reason.TransportKind != "Timeout" {
		t.Fatalf("timeout kind = %v", timeout.Reason.TransportKind)
	}

	refused := transportError(req, errors.New("connection refused"))
	if refused.Reason.TransportKind == nil || *refused.Reason.TransportKind != "TransportError" {
		t.Fatalf("kind = %v", refused.Reason.TransportKind)
	}
	if refused.Reason.URL == nil || strings.Contains(*refused.Reason.URL, "secret") {
		t.Fatalf("url = %v", refused.Reason.URL)
	}
}
