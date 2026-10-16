package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// memStore is a minimal in-memory MediaStore for the file-exchange tests.
type memStore struct {
	objects map[string][]byte
	slot    *schema.UploadTarget
}

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Put(_ context.Context, media schema.MediaUpload) (schema.Reference, error) {
	ref := "https://store/" + strconv.Itoa(len(m.objects))
	m.objects[ref] = media.Data
	return schema.Reference{Source: "url", Value: ref}, nil
}

func (m *memStore) Get(_ context.Context, ref schema.Reference) (io.ReadCloser, error) {
	b, ok := m.objects[ref.Value]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) Target() schema.UploadTarget {
	return schema.UploadTarget{URL: "https://store/", Method: http.MethodPut}
}

func (m *memStore) Reserve(context.Context, string, string, int64) (schema.UploadTarget, error) {
	if m.slot != nil {
		return *m.slot, nil
	}
	return schema.UploadTarget{}, nil
}

func testServer(t *testing.T, ingest *Ingest) *Server {
	t.Helper()
	srv, err := New(Options{
		Factory: &fakeFactory{},
		Agents:  []schema.AgentInfo{{ID: "build", Expose: &schema.AgentExpose{Name: "Build", Description: "d"}}},
		BaseURL: "https://agent.example",
		Ingest:  ingest,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func TestCardAdvertisesStorageExtension(t *testing.T) {
	srv := testServer(t, &Ingest{
		Store: newMemStore(), Schemes: []string{"https"},
		MediaTypes: []string{"image/*"}, MaxInlineBytes: 1024, MaxBytes: 2048,
	})
	card := srv.Cards(nil)[0]
	var ext *a2a.AgentExtension
	for i := range card.Capabilities.Extensions {
		if card.Capabilities.Extensions[i].URI == StorageExtensionURI {
			ext = &card.Capabilities.Extensions[i]
		}
	}
	if ext == nil {
		t.Fatalf("storage extension missing: %+v", card.Capabilities.Extensions)
	}
	ingest, ok := ext.Params["ingest"].(map[string]any)
	if !ok {
		t.Fatalf("ingest params = %+v", ext.Params)
	}
	if ingest["maxInlineBytes"] != int64(1024) || ingest["maxBytes"] != int64(2048) {
		t.Fatalf("ingest limits = %+v", ingest)
	}
	modes, _ := ingest["modes"].([]map[string]any)
	if len(modes) != 2 {
		t.Fatalf("modes = %+v, want reference+broker", ingest["modes"])
	}
	seen := map[any]bool{}
	for _, m := range modes {
		seen[m["mode"]] = true
	}
	if !seen["reference"] || !seen["broker"] {
		t.Fatalf("modes = %+v", modes)
	}
}

func TestIngestSlotUnsupported(t *testing.T) {
	srv := testServer(t, &Ingest{Store: newMemStore()})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agents/build/storage/in/?name=a.png", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("slot status = %d, want 501", rec.Code)
	}
}

func TestIngestSlotBroker(t *testing.T) {
	store := newMemStore()
	store.slot = &schema.UploadTarget{URL: "https://store.example/put?x=1", Method: http.MethodPut, Reference: "https://store.example/obj"}
	srv := testServer(t, &Ingest{Store: store})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agents/build/storage/in/?name=a.png&mime=image/png&size=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("slot status = %d body=%s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["url"] != "https://store.example/put?x=1" || out["reference"] != "https://store.example/obj" {
		t.Fatalf("slot = %+v", out)
	}
}

func TestNoStorageExtensionWithoutIngest(t *testing.T) {
	card := testServer(t, nil).Cards(nil)[0]
	if len(card.Capabilities.Extensions) != 0 {
		t.Fatalf("unexpected extensions: %+v", card.Capabilities.Extensions)
	}
}

func TestIngestPutThenGet(t *testing.T) {
	srv := testServer(t, &Ingest{Store: newMemStore(), MaxBytes: 1024})
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/agents/build/storage/in/", strings.NewReader("hello")))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["reference"] == "" {
		t.Fatalf("no reference in %s", rec.Body)
	}

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/agents/build/storage/in/?ref="+url.QueryEscape(out["reference"]), nil))
	if get.Code != http.StatusOK || get.Body.String() != "hello" {
		t.Fatalf("GET status=%d body=%q", get.Code, get.Body.String())
	}
}

func TestIngestPutTooLargeExplainsLimit(t *testing.T) {
	srv := testServer(t, &Ingest{Store: newMemStore(), MaxBytes: 4})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/agents/build/storage/in/", strings.NewReader("way too large"))
	req.Header.Set("Content-Type", "image/png")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "4-byte limit") {
		t.Fatalf("body = %q, want the limit in the message", rec.Body.String())
	}
}

func TestIngestPutRejectsMediaTypeExplainsAllowed(t *testing.T) {
	srv := testServer(t, &Ingest{Store: newMemStore(), MediaTypes: []string{"image/*"}, MaxBytes: 1 << 20})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/agents/build/storage/in/", strings.NewReader("x"))
	req.Header.Set("Content-Type", "application/pdf")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "image/*") {
		t.Fatalf("body = %q, want the allowed types", rec.Body.String())
	}
}
