package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestHTTPAuthKinds checks basic/bearer credentials reach writes.
func TestHTTPAuthKinds(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.StorageConfig
		want string
	}{
		{"basic", config.StorageConfig{Auth: "basic", Username: "u", Password: "p"}, "Basic dTpw"},
		{"bearer", config.StorageConfig{Auth: "bearer", Token: "tok"}, "Bearer tok"},
		{"custom", config.StorageConfig{Header: "X-Api-Key", Value: "k"}, "k"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var putAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key := "Authorization"
				if tc.cfg.Header != "" {
					key = tc.cfg.Header
				}
				putAuth = r.Header.Get(key)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			cfg := tc.cfg
			cfg.Backend, cfg.URL = config.StorageHTTP, srv.URL+"/"
			store, err := New(&cfg)
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			name := "a.png"
			_, err = store.Put(context.Background(), schema.MediaUpload{Name: &name, Data: []byte("x")})
			if err != nil {
				t.Fatalf("put: %v", err)
			}
			if putAuth != tc.want {
				t.Fatalf("put auth = %q, want %q", putAuth, tc.want)
			}
		})
	}
}

// TestSecureLinkMD5Vector checks the nginx secure_link signer against an
// independently computed value.
func TestSecureLinkMD5Vector(t *testing.T) {
	const want = "RzH1Xg57grOs26QeMmfa9w"
	if got := secureLinkMD5("/base/abc", 1700000000, "s3cr3t"); got != want {
		t.Fatalf("secureLinkMD5 = %q, want %q", got, want)
	}
}

// TestHTTPPresign checks a PresignSecret makes Put return a signed url.
func TestHTTPPresign(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store, err := New(&config.StorageConfig{Backend: config.StorageHTTP, URL: srv.URL + "/base/", PresignSecret: "s3cr3t", PresignTTL: "5m"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Name: &name, Data: []byte("x")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !strings.Contains(ref.Value, "md5=") || !strings.Contains(ref.Value, "expires=") || ref.ExpiresAt == nil {
		t.Fatalf("presigned ref = %+v", ref)
	}
}

func TestStoragePut(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store, err := New(&config.StorageConfig{Backend: config.StorageHTTP, URL: srv.URL + "/base/", Header: "Authorization", Value: "Bearer token"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Mime: "image/png", Name: &name, Data: []byte("bytes")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.Source != "url" || gotBody != "bytes" {
		t.Fatalf("ref = %+v, body = %q", ref, gotBody)
	}
	// The object name is content-addressed and suffixed with the file name.
	if want := "/base/"; len(ref.Value) <= len(srv.URL)+len(want) || ref.Value[:len(srv.URL)+len(want)] != srv.URL+want {
		t.Fatalf("value = %q", ref.Value)
	}
}
