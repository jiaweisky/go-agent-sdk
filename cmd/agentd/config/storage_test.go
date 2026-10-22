package config

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestHTTPAuthKinds checks basic/bearer credentials reach writes.
func TestHTTPAuthKinds(t *testing.T) {
	cases := []struct {
		name string
		cfg  StorageConfig
		want string
	}{
		{"basic", StorageConfig{Auth: "basic", Username: "u", Password: "p"}, "Basic dTpw"},
		{"bearer", StorageConfig{Auth: "bearer", Token: "tok"}, "Bearer tok"},
		{"custom", StorageConfig{Header: "X-Api-Key", Value: "k"}, "k"},
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
			cfg.Backend, cfg.URL = "http", srv.URL+"/"
			store := cfg.Store()
			name := "a.png"
			_, err := store.Put(context.Background(), schema.MediaUpload{Name: &name, Data: []byte("x")})
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

	store := StorageConfig{Backend: "http", URL: srv.URL + "/base/", PresignSecret: "s3cr3t", PresignTTL: "5m"}.Store()

	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Name: &name, Data: []byte("x")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !strings.Contains(ref.Value, "md5=") || !strings.Contains(ref.Value, "expires=") || ref.ExpiresAt == nil {
		t.Fatalf("presigned ref = %+v", ref)
	}
}

func TestValidateHTTPAuth(t *testing.T) {
	for _, c := range []StorageConfig{
		{URL: "https://x/", Auth: "basic"},
		{URL: "https://x/", Auth: "bearer"},
		{URL: "https://x/", Auth: "digest"},
		{URL: "https://x/", Header: "X-K"},
	} {
		if err := ValidateStorage(&c); err == nil {
			t.Fatalf("expected rejection: %+v", c)
		}
	}
	for _, c := range []StorageConfig{
		{URL: "https://x/"},
		{URL: "https://x/", Auth: "none"},
		{URL: "https://x/", Auth: "basic", Username: "u", Password: "p"},
		{URL: "https://x/", Auth: "bearer", Token: "t"},
		{URL: "https://x/", Header: "X-K", Value: "v"},
	} {
		if err := ValidateStorage(&c); err != nil {
			t.Fatalf("expected valid: %+v: %v", c, err)
		}
	}
}

func TestValidateStorage(t *testing.T) {
	if err := ValidateStorage(nil); err != nil {
		t.Fatalf("nil storage should be valid: %v", err)
	}
	if err := ValidateStorage(&StorageConfig{URL: "https://store.example/x/"}); err != nil {
		t.Fatalf("http storage with url should be valid: %v", err)
	}
	if err := ValidateStorage(&StorageConfig{}); err == nil {
		t.Fatal("storage without a url must be rejected")
	}
	if err := ValidateStorage(&StorageConfig{Backend: "local"}); err == nil {
		t.Fatal("local storage without a root must be rejected")
	}
	if err := ValidateStorage(&StorageConfig{Backend: "local", Root: "/shared/media"}); err != nil {
		t.Fatalf("local storage with a root should be valid: %v", err)
	}
	if err := ValidateStorage(&StorageConfig{Backend: "ftp", URL: "https://x"}); err == nil {
		t.Fatal("an unknown backend must be rejected")
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

	store := StorageConfig{Backend: "http", URL: srv.URL + "/base/", Header: "Authorization", Value: "Bearer token"}.Store()
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

func TestLocalStorePut(t *testing.T) {
	dir := t.TempDir()
	store := StorageConfig{Backend: "local", Root: dir, ServeBaseURL: "https://media.example/pub"}.Store()
	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Mime: "image/png", Name: &name, Data: []byte("bytes")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.Value[:len("https://media.example/pub/")] != "https://media.example/pub/" {
		t.Fatalf("value = %q", ref.Value)
	}
}
