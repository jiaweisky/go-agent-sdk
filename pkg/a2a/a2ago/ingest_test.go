package a2ago

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/jiaweisky/go-agent-sdk/pkg/a2a/client"
)

// clientWithIngest builds a goClient whose card advertises the given ingest.
func clientWithIngest(ingest *client.Ingest) *goClient {
	return &goClient{card: client.Card{Ingest: ingest}}
}

func TestFilePartUploadsLargePayload(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"reference":"https://store/abc"}`)
	}))
	defer srv.Close()

	gc := clientWithIngest(&client.Ingest{UploadURL: srv.URL, MaxInlineBytes: 2})
	part := gc.filePart(context.Background(), client.FileAttachment{MIME: "image/png", Name: "a.png", Data: []byte("bigdata")})

	if string(got) != "bigdata" {
		t.Fatalf("uploaded %q, want bigdata", got)
	}
	if url, ok := part.Content.(a2a.URL); !ok || string(url) != "https://store/abc" {
		t.Fatalf("part content = %#v, want the reference url", part.Content)
	}
}

func TestFilePartKeepsSmallPayloadInline(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	gc := clientWithIngest(&client.Ingest{UploadURL: srv.URL, MaxInlineBytes: 1024})
	part := gc.filePart(context.Background(), client.FileAttachment{MIME: "image/png", Data: []byte("hi")})
	if called {
		t.Fatal("a small payload should not be uploaded")
	}
	if raw, ok := part.Content.(a2a.Raw); !ok || string(raw) != "hi" {
		t.Fatalf("part content = %#v, want inline bytes", part.Content)
	}
}

func TestFilePartUsesBrokerSlot(t *testing.T) {
	var slotBody []byte
	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer putSrv.Close()
	brokerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"`+putSrv.URL+`","method":"PUT","reference":"https://store/obj"}`)
	}))
	defer brokerSrv.Close()

	gc := clientWithIngest(&client.Ingest{BrokerURL: brokerSrv.URL, MaxInlineBytes: 2})
	part := gc.filePart(context.Background(), client.FileAttachment{MIME: "image/png", Name: "a.png", Data: []byte("bigdata")})
	if string(slotBody) != "bigdata" {
		t.Fatalf("slot upload body = %q", slotBody)
	}
	if url, ok := part.Content.(a2a.URL); !ok || string(url) != "https://store/obj" {
		t.Fatalf("part content = %#v, want the slot reference", part.Content)
	}
}

// TestFilePartSendsHeadersToIngest checks the client's auth headers reach the
// remote's ingest endpoints (which the transport's interceptor does not cover).
func TestFilePartSendsHeadersToIngest(t *testing.T) {
	var referenceAuth, brokerAuth string
	putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer putSrv.Close()
	referenceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		referenceAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"reference":"https://store/abc"}`)
	}))
	defer referenceSrv.Close()
	brokerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		brokerAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"`+putSrv.URL+`","method":"PUT","reference":"https://store/obj"}`)
	}))
	defer brokerSrv.Close()

	headers := map[string]string{"Authorization": "Bearer t"}
	file := client.FileAttachment{MIME: "image/png", Name: "a.png", Data: []byte("bigdata")}

	gc := clientWithIngest(&client.Ingest{UploadURL: referenceSrv.URL, MaxInlineBytes: 2})
	gc.headers = headers
	gc.filePart(context.Background(), file)
	if referenceAuth != "Bearer t" {
		t.Fatalf("reference ingest auth = %q", referenceAuth)
	}

	gc = clientWithIngest(&client.Ingest{BrokerURL: brokerSrv.URL, MaxInlineBytes: 2})
	gc.headers = headers
	gc.filePart(context.Background(), file)
	if brokerAuth != "Bearer t" {
		t.Fatalf("broker ingest auth = %q", brokerAuth)
	}
}
