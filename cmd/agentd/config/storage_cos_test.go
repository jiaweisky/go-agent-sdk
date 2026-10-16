package config

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestCOSSignatureVector checks the native COS signer against a signature
// computed independently from the documented q-sign-algorithm=sha1 scheme.
func TestCOSSignatureVector(t *testing.T) {
	store := cosStore{config: StorageConfig{COS: &COSConfig{
		Region: "ap-guangzhou", Bucket: "examplebucket-1250000000",
		SecretID: "AKIDEXAMPLE", SecretKey: "SECRETEXAMPLE",
	}}}
	store.now = func() time.Time { return time.Unix(1700000000, 0) }

	keyTime, signature, paramList := store.signature("PUT", "abc.png", nil, time.Unix(1700003600, 0))
	if keyTime != "1700000000;1700003600" {
		t.Fatalf("key time = %q", keyTime)
	}
	if paramList != "" {
		t.Fatalf("param list = %q", paramList)
	}
	const want = "ff4d230c789e596fd520594c60ebd8255627f9c2"
	if signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func TestCOSStoreRoundTrip(t *testing.T) {
	var putAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			putAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Write([]byte("stored"))
		}
	}))
	defer srv.Close()

	store := StorageConfig{Backend: "cos", COS: &COSConfig{
		Endpoint: srv.URL, Region: "ap-guangzhou", Bucket: "b",
		SecretID: "id", SecretKey: "key", PresignTTL: "5m",
	}}.Store()

	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Mime: "image/png", Name: &name, Data: []byte("bytes")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !strings.Contains(putAuth, "q-sign-algorithm=sha1") || !strings.Contains(putAuth, "q-ak=id") {
		t.Fatalf("put missing native signature: %q", putAuth)
	}
	if !strings.Contains(ref.Value, "q-signature=") {
		t.Fatalf("reference should be a presigned url: %q", ref.Value)
	}
	if ref.ExpiresAt == nil {
		t.Fatal("presigned reference should carry an expiry")
	}
	rc, err := store.Get(context.Background(), schema.Reference{Source: "url", Value: srv.URL + "/b/key"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	if b, _ := io.ReadAll(rc); string(b) != "stored" {
		t.Fatalf("get body = %q", b)
	}
}

func TestCOSReserveSignsPutSlot(t *testing.T) {
	store := StorageConfig{Backend: "cos", COS: &COSConfig{
		Region: "ap-guangzhou", Bucket: "b", SecretID: "id", SecretKey: "key", PresignTTL: "5m",
	}}.Store()

	target, err := store.Reserve(context.Background(), "a.png", "image/png", 10)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if !strings.Contains(target.URL, "q-signature=") || target.Method != http.MethodPut {
		t.Fatalf("reserve target = %+v", target)
	}
	if target.Reference == "" || target.Expires.IsZero() {
		t.Fatalf("reserve reference/expiry = %+v", target)
	}
	if target.Headers["Content-Type"] != "image/png" {
		t.Fatalf("reserve headers = %+v", target.Headers)
	}
}

// TestCOSAnonymous checks an uncredentialed bucket signs nothing and returns a
// plain url.
func TestCOSAnonymous(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method == http.MethodGet {
			w.Write([]byte("stored"))
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	store := StorageConfig{Backend: "cos", COS: &COSConfig{Endpoint: srv.URL, Region: "r", Bucket: "b"}}.Store()
	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Name: &name, Data: []byte("x")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if auth != "" {
		t.Fatalf("anonymous put must not sign: %q", auth)
	}
	if strings.Contains(ref.Value, "q-signature") || ref.ExpiresAt != nil {
		t.Fatalf("anonymous ref should be a plain url: %+v", ref)
	}
	if target, _ := store.Reserve(context.Background(), "a.png", "image/png", 3); target.URL != "" {
		t.Fatalf("anonymous reserve should be empty: %+v", target)
	}
	rc, err := store.Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	if auth != "" {
		t.Fatalf("anonymous get must not sign: %q", auth)
	}
}

func TestValidateCOS(t *testing.T) {
	if err := ValidateStorage(&StorageConfig{Backend: "cos"}); err == nil {
		t.Fatal("cos without a config must be rejected")
	}
	if err := ValidateStorage(&StorageConfig{Backend: "cos", COS: &COSConfig{Region: "ap-guangzhou", Bucket: "b"}}); err != nil {
		t.Fatalf("anonymous cos rejected: %v", err)
	}
	if err := ValidateStorage(&StorageConfig{Backend: "cos", COS: &COSConfig{Region: "ap-guangzhou", Bucket: "b", SecretID: "id"}}); err == nil {
		t.Fatal("cos with half a credential must be rejected")
	}
	if err := ValidateStorage(&StorageConfig{Backend: "cos", COS: &COSConfig{Region: "ap-guangzhou", Bucket: "b", SecretID: "a", SecretKey: "s"}}); err != nil {
		t.Fatalf("valid cos rejected: %v", err)
	}
}
