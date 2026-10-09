package serveconfig

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func TestMediaUploaderPostsMultipartAndReadsRef(t *testing.T) {
	type capture struct {
		method, path, auth, name, field string
		data                            []byte
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
			got.data, _ = io.ReadAll(file)
		}
		got.field = r.FormValue("purpose")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"file_id":"file-123"}`)
	}))
	defer srv.Close()

	uploader := MediaUploadConfig{
		URL:       srv.URL + "/files",
		Header:    "Authorization",
		Value:     "Bearer tok",
		Fields:    map[string]string{"purpose": "vision"},
		IDField:   "file_id",
		ExpiresIn: 60,
	}.Uploader()

	name := "red.png"
	before := time.Now()
	ref, err := uploader.Upload(context.Background(), schema.MediaUpload{
		Mime: "image/png", Name: &name, Data: []byte("PNGBYTES"),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if ref.Source != "file" || ref.ID != "file-123" {
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
	if got.field != "vision" {
		t.Fatalf("extra field = %q", got.field)
	}
}

func TestMediaUploaderErrors(t *testing.T) {
	if _, err := (MediaUploadConfig{}).Uploader().Upload(context.Background(), schema.MediaUpload{Data: []byte("x")}); err == nil {
		t.Fatal("expected an error when url is unset")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := (MediaUploadConfig{URL: srv.URL}).Uploader().Upload(context.Background(), schema.MediaUpload{Data: []byte("x")}); err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer empty.Close()
	if _, err := (MediaUploadConfig{URL: empty.URL}).Uploader().Upload(context.Background(), schema.MediaUpload{Data: []byte("x")}); err == nil {
		t.Fatal("expected an error when the response has no id")
	}
}
