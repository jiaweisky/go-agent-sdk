package config

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// TestSigV4MatchesAWSVector checks the header signer against AWS's published S3
// "GET Object" example.
func TestSigV4MatchesAWSVector(t *testing.T) {
	sig := sigV4{
		accessKey: "AKIAIOSFODNN7EXAMPLE",
		secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		region:    "us-east-1", service: "s3",
		now: func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) },
	}
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	sig.signHeader(req, nil)
	const want = "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if !strings.HasSuffix(req.Header.Get("Authorization"), "Signature="+want) {
		t.Fatalf("authorization = %q, want Signature=%s", req.Header.Get("Authorization"), want)
	}
}

func TestS3StoreRoundTrip(t *testing.T) {
	var putBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			putBody, _ = io.ReadAll(r.Body)
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
				t.Errorf("missing signature: %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Write([]byte("stored"))
		}
	}))
	defer srv.Close()

	store := StorageConfig{Backend: "s3", S3: &S3Config{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: "b",
		AccessKey: "AK", SecretKey: "SK", PathStyle: true, PresignTTL: "5m",
	}}.Store()

	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Mime: "image/png", Name: &name, Data: []byte("bytes")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if string(putBody) != "bytes" {
		t.Fatalf("put body = %q", putBody)
	}
	if !strings.Contains(ref.Value, "X-Amz-Signature=") {
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

// TestS3Anonymous checks an uncredentialed bucket signs nothing and returns a
// plain url.
func TestS3Anonymous(t *testing.T) {
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

	store := StorageConfig{Backend: "s3", S3: &S3Config{Endpoint: srv.URL, Region: "r", Bucket: "b", PathStyle: true}}.Store()
	name := "a.png"
	ref, err := store.Put(context.Background(), schema.MediaUpload{Name: &name, Data: []byte("x")})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if auth != "" {
		t.Fatalf("anonymous put must not sign: %q", auth)
	}
	if strings.Contains(ref.Value, "X-Amz-Signature") || ref.ExpiresAt != nil {
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

func TestValidateS3(t *testing.T) {
	if err := ValidateStorage(&StorageConfig{Backend: "s3"}); err == nil {
		t.Fatal("s3 without a config must be rejected")
	}
	if err := ValidateStorage(&StorageConfig{Backend: "s3", S3: &S3Config{Endpoint: "https://s3", Region: "r", Bucket: "b"}}); err != nil {
		t.Fatalf("anonymous s3 rejected: %v", err)
	}
	if err := ValidateStorage(&StorageConfig{Backend: "s3", S3: &S3Config{Endpoint: "https://s3", Region: "r", Bucket: "b", AccessKey: "a"}}); err == nil {
		t.Fatal("s3 with half a credential must be rejected")
	}
	if err := ValidateStorage(&StorageConfig{Backend: "s3", S3: &S3Config{Endpoint: "https://s3", Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s"}}); err != nil {
		t.Fatalf("valid s3 rejected: %v", err)
	}
}

// TestCanonicalURISingleEncodes guards the SigV4 canonical URI against
// double-encoding a key that needs escaping (a pre-escaped path would become
// e.g. %25C3%25A9).
func TestCanonicalURISingleEncodes(t *testing.T) {
	u, err := url.Parse("https://b.s3.example/caf%C3%A9%20file.png")
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicalURI(u); got != "/caf%C3%A9%20file.png" {
		t.Fatalf("canonical uri = %q, want a single encoding", got)
	}
}

// TestS3OwnsRejectsForeignBucket: under path-style addressing a reference to
// another bucket on the same endpoint is not this store's.
func TestS3OwnsRejectsForeignBucket(t *testing.T) {
	store := StorageConfig{Backend: "s3", S3: &S3Config{
		Endpoint: "https://s3.example", Region: "us-east-1", Bucket: "b",
		AccessKey: "AK", SecretKey: "SK", PathStyle: true,
	}}.Store().(s3Store)

	own := func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return store.owns(u)
	}
	if own("https://s3.example/other/key") {
		t.Fatal("another bucket must not be owned")
	}
	if !own("https://s3.example/b/key") {
		t.Fatal("this bucket's object must be owned")
	}
}

// TestS3GetForeignBucketWrapsUnsupportedReference: Get of a foreign reference
// fails with an error that errors.Is detects as ErrUnsupportedReference.
func TestS3GetForeignBucketWrapsUnsupportedReference(t *testing.T) {
	store := StorageConfig{Backend: "s3", S3: &S3Config{
		Endpoint: "https://s3.example", Region: "us-east-1", Bucket: "b",
		AccessKey: "AK", SecretKey: "SK", PathStyle: true,
	}}.Store()
	_, err := store.Get(context.Background(), schema.Reference{Source: "url", Value: "https://s3.example/other/key"})
	if !errors.Is(err, schema.ErrUnsupportedReference) {
		t.Fatalf("Get foreign ref err = %v, want ErrUnsupportedReference", err)
	}
}
