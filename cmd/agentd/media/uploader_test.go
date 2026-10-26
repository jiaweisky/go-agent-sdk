package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func profile(url string) *config.ProviderFileAPI {
	return &config.ProviderFileAPI{
		Kind:   "multipart-json",
		URL:    url,
		Auth:   config.ProviderFileAuth{Headers: map[string]string{"Authorization": "Bearer tok"}},
		Fields: map[string]string{"purpose": "vision"},
		Expiry: config.ProviderFileExpiry{Style: "none"},
		MIME:   config.ProviderFileMIME{Allowed: []string{"image/png"}},
		Limits: config.ProviderFileLimits{MaxBytes: 1 << 20, Timeout: "2m"},
	}
}

func newUploader(t *testing.T, cfg *config.ProviderFileAPI) schema.MediaUploader {
	t.Helper()
	uploader, err := NewUploader("test", cfg)
	if err != nil {
		t.Fatalf("new uploader: %v", err)
	}
	return uploader
}

func TestMediaUploaderPostsMultipartAndReadsRef(t *testing.T) {
	type capture struct {
		method, path, auth, name, field, contentType string
		data                                         []byte
	}
	var got capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		got.auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if file, header, err := r.FormFile("file"); err == nil {
			got.name = header.Filename
			got.contentType = header.Header.Get("Content-Type")
			got.data, _ = io.ReadAll(file)
		}
		got.field = r.FormValue("purpose")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"file_id":"file-123"}`)
	}))
	defer srv.Close()

	cfg := profile(srv.URL + "/files")
	cfg.IDField = "file_id"
	cfg.Expiry = config.ProviderFileExpiry{Style: "expires_after", Seconds: 60}
	uploader := newUploader(t, cfg)

	name := "red.png"
	before := time.Now()
	ref, err := uploader.Upload(context.Background(), schema.MediaUpload{
		Mime: "image/png", Name: &name, Data: []byte("PNGBYTES"),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if ref.Source != "file" || ref.Value != "file-123" {
		t.Fatalf("ref = %+v, want file/file-123", ref)
	}
	if ref.ExpiresAt == nil || ref.ExpiresAt.Before(before.Add(59*time.Second)) || ref.ExpiresAt.After(before.Add(61*time.Second)) {
		t.Fatalf("expiresAt = %v, want ~now+60s", ref.ExpiresAt)
	}
	if got.method != http.MethodPost || got.path != "/files" || got.auth != "Bearer tok" {
		t.Fatalf("request = %+v", got)
	}
	if got.name != "red.png" || string(got.data) != "PNGBYTES" {
		t.Fatalf("file part = %q %q", got.name, got.data)
	}
	if got.contentType != "image/png" {
		t.Fatalf("file content type = %q, want image/png", got.contentType)
	}
	if got.field != "vision" {
		t.Fatalf("extra field = %q", got.field)
	}
}

func TestMediaUploaderErrors(t *testing.T) {
	if _, err := NewUploader("test", &config.ProviderFileAPI{}); err == nil {
		t.Fatal("expected an error for a malformed profile")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := newUploader(t, profile(srv.URL)).Upload(context.Background(), schema.MediaUpload{Data: []byte("x")}); err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer empty.Close()
	if _, err := newUploader(t, profile(empty.URL)).Upload(context.Background(), schema.MediaUpload{Data: []byte("x")}); err == nil {
		t.Fatal("expected an error when the response has no id")
	}
}

func TestMediaUploaderLifetime(t *testing.T) {
	cfg := profile("https://up.example")
	cfg.Expiry = config.ProviderFileExpiry{Style: "expires_after", Seconds: 60}
	client := uploadClient{config: cfg}
	if got := client.referenceLifetime(); got != 60 {
		t.Fatalf("lifetime = %d, want 60", got)
	}
	cfg.Expiry = config.ProviderFileExpiry{Style: "none"}
	if got := client.referenceLifetime(); got != 0 {
		t.Fatalf("none lifetime = %d, want 0", got)
	}
}

func TestMediaUploaderSendsExpiresAfter(t *testing.T) {
	var anchor, seconds string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		anchor = r.FormValue("expires_after[anchor]")
		seconds = r.FormValue("expires_after[seconds]")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"f1"}`)
	}))
	defer srv.Close()

	cfg := profile(srv.URL)
	cfg.Expiry = config.ProviderFileExpiry{Style: "expires_after", Seconds: 3600}
	ref, err := newUploader(t, cfg).Upload(context.Background(), schema.MediaUpload{Mime: "image/png", Data: []byte("x")})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if anchor != "created_at" || seconds != "3600" {
		t.Fatalf("expires_after = %q/%q, want created_at/3600", anchor, seconds)
	}
	if ref.ExpiresAt == nil {
		t.Fatal("expiry style should bound the local reference cache")
	}
}

func TestMediaUploaderCapabilities(t *testing.T) {
	cfg := profile("https://up.example")
	cfg.MIME.Allowed = []string{"image/png", "application/pdf"}
	cfg.Limits.MaxBytes = 4096
	uploader := newUploader(t, cfg)
	reporter, ok := uploader.(schema.MediaCapabilityReporter)
	if !ok {
		t.Fatal("uploader should report media capabilities")
	}
	caps := reporter.MediaCapabilities()
	if len(caps.MIMEs) != 2 || caps.MIMEs[1] != "application/pdf" || caps.MaxBytes != 4096 {
		t.Fatalf("capabilities = %+v", caps)
	}
}

func TestMediaUploaderMaxBytesAndDottedID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"file":{"id":"nested-1"}}}`)
	}))
	defer srv.Close()

	cfg := profile(srv.URL)
	cfg.IDField = "data.file.id"
	cfg.Limits.MaxBytes = 4
	uploader := newUploader(t, cfg)
	ref, err := uploader.Upload(context.Background(), schema.MediaUpload{Mime: "image/png", Data: []byte("1234")})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if ref.Value != "nested-1" {
		t.Fatalf("dotted id = %q", ref.Value)
	}
	if _, err := uploader.Upload(context.Background(), schema.MediaUpload{Mime: "image/png", Data: []byte("12345")}); err == nil {
		t.Fatal("a payload above maxBytes must be rejected before upload")
	}
}

func TestNewUploaderNilAndTimeout(t *testing.T) {
	if uploader, err := NewUploader("test", nil); uploader != nil || err != nil {
		t.Fatalf("nil profile = %v, %v; want nil, nil", uploader, err)
	}
	cfg := profile("https://up.example")
	cfg.Limits.Timeout = "1500ms"
	uploader, err := NewUploader("test", cfg)
	if err != nil {
		t.Fatalf("new uploader: %v", err)
	}
	client, ok := uploader.(uploadClient)
	if !ok {
		t.Fatalf("uploader type = %T", uploader)
	}
	if client.client.Timeout != 1500*time.Millisecond {
		t.Fatalf("timeout = %v, want 1.5s", client.client.Timeout)
	}
	cfg.Limits.Timeout = "not-a-duration"
	if _, err := NewUploader("test", cfg); err == nil {
		t.Fatal("a malformed timeout must be rejected")
	}
}
