package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/go-agent-sdk/llm/route"
	"github.com/user/go-agent-sdk/llm/schema"
)

// TestJSONRequestPartsAppliesEndpointQuery guards that Endpoint.Query (Azure
// api-version, OpenAI QueryParams) is not silently dropped.
func TestJSONRequestPartsAppliesEndpointQuery(t *testing.T) {
	parts, err := jsonRequestParts(route.TransportPrepareInput{
		Body:    map[string]any{},
		Request: &schema.LLMRequest{},
		Endpoint: route.Endpoint{
			BaseURL: "https://example.test/",
			Path:    func(route.EndpointInput) string { return "/v1/chat" },
			Query:   map[string]string{"api-version": "2024-02-01"},
		},
		EncodeBody: func(any) string { return "{}" },
	})
	if err != nil {
		t.Fatalf("jsonRequestParts: %v", err)
	}
	if !strings.Contains(parts.URL, "api-version=2024-02-01") {
		t.Fatalf("endpoint query dropped: %q", parts.URL)
	}
	if !strings.HasPrefix(parts.URL, "https://example.test/v1/chat") {
		t.Fatalf("unexpected URL: %q", parts.URL)
	}
}

// TestHTTPTransportStreamsFrames verifies the transport yields each SSE frame as
// it arrives instead of buffering the whole response body.
func TestHTTPTransportStreamsFrames(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	releaseFn := func() { once.Do(func() { close(release) }) }
	defer releaseFn()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fmt.Fprint(w, "data: one\n\n")
		flusher.Flush()
		<-release // hold the second frame until the test observed the first
		fmt.Fprint(w, "data: two\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	transport := NewHttpJsonTransport(HttpJsonInput{Framing: route.SSE})
	prepared := HttpPrepared{URL: srv.URL, Method: "POST", Body: "{}", Framing: route.SSE}
	runtime := route.TransportRuntime{HTTP: route.NewExecutor(srv.Client())}

	first := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for frame, err := range transport.Frames(context.Background(), prepared, nil, runtime) {
			if err != nil {
				t.Errorf("frame error: %v", err)
				return
			}
			select {
			case first <- frame.(string):
			default:
			}
		}
	}()

	select {
	case got := <-first:
		if got != "one" {
			t.Fatalf("first frame = %q, want %q", got, "one")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first frame was not streamed before the response completed")
	}
	releaseFn()
	<-done
}

// TestHTTPTransportCancelStopsStream verifies cancelling the context aborts an
// in-flight provider request and surfaces context.Canceled instead of retrying
// or hanging.
func TestHTTPTransportCancelStopsStream(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fmt.Fprint(w, "data: one\n\n")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	transport := NewHttpJsonTransport(HttpJsonInput{Framing: route.SSE})
	prepared := HttpPrepared{URL: srv.URL, Method: "POST", Body: "{}", Framing: route.SSE}
	runtime := route.TransportRuntime{HTTP: route.NewExecutor(srv.Client())}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		for _, err := range transport.Frames(ctx, prepared, nil, runtime) {
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop after context cancellation")
	}
}

func TestJSONRequestPartsOverlaysStructBody(t *testing.T) {
	body := struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}{Model: "test-model", Stream: true}
	request := &schema.LLMRequest{Http: &schema.HttpOptions{Body: schema.JsonSchema{"safety_settings": float64(5)}}}

	parts, err := jsonRequestParts(route.TransportPrepareInput{
		Body:    body,
		Request: request,
		Endpoint: route.Endpoint{
			BaseURL: "https://example.test",
			Path:    func(route.EndpointInput) string { return "/v1/chat" },
		},
		EncodeBody: func(b any) string { return "{}" },
	})
	if err != nil {
		t.Fatalf("jsonRequestParts: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(parts.BodyText), &decoded); err != nil {
		t.Fatalf("unmarshal %q: %v", parts.BodyText, err)
	}
	if decoded["model"] != "test-model" || decoded["stream"] != true || decoded["safety_settings"] != float64(5) {
		t.Fatalf("body = %s", parts.BodyText)
	}
	if parts.Headers["content-type"] != "application/json" {
		t.Fatalf("content-type = %q", parts.Headers["content-type"])
	}
}

func TestJSONRequestPartsRejectsProtocolOwnedOverlay(t *testing.T) {
	request := &schema.LLMRequest{Http: &schema.HttpOptions{Body: schema.JsonSchema{"model": "other"}}}
	_, err := jsonRequestParts(route.TransportPrepareInput{
		Body:    map[string]any{"model": "test"},
		Request: request,
		Endpoint: route.Endpoint{
			BaseURL: "https://example.test",
			Path:    func(route.EndpointInput) string { return "/v1/chat" },
		},
		EncodeBody: func(b any) string { return "{}" },
	})
	if err == nil {
		t.Fatal("want an error for overlaying a protocol-owned field")
	}
}
