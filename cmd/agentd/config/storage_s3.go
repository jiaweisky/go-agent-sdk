package config

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// S3Config configures an S3-compatible object store (AWS S3, Tencent COS, MinIO,
// ...). Requests are signed with AWS Signature Version 4.
type S3Config struct {
	// Endpoint is the base URL, e.g. https://s3.us-east-1.amazonaws.com or a COS
	// endpoint (https://cos.ap-guangzhou.myqcloud.com).
	Endpoint string `yaml:"endpoint"`
	// Region is the signing region (e.g. us-east-1, ap-guangzhou).
	Region string `yaml:"region"`
	// Bucket is the target bucket.
	Bucket string `yaml:"bucket"`
	// AccessKey / SecretKey are the signing credentials.
	AccessKey string `yaml:"accessKey"`
	SecretKey string `yaml:"secretKey"`
	// PathStyle addresses objects as endpoint/bucket/key (MinIO/older COS) rather
	// than bucket.endpoint/key.
	PathStyle bool `yaml:"pathStyle"`
	// PresignTTL bounds a returned presigned GET url's lifetime.
	// Empty defaults to 15m.
	PresignTTL string `yaml:"presignTTL"`
}

// HasCredentials reports whether the bucket has a signing key pair; without one
// it is public and requests are unsigned.
func (c S3Config) HasCredentials() bool { return c.AccessKey != "" && c.SecretKey != "" }

// ValidateS3 rejects a malformed S3 backend.
func ValidateS3(s *S3Config) error {
	switch {
	case s.Endpoint == "":
		return fmt.Errorf("storage.s3.endpoint is required")
	case s.Region == "":
		return fmt.Errorf("storage.s3.region is required")
	case s.Bucket == "":
		return fmt.Errorf("storage.s3.bucket is required")
	case (s.AccessKey == "") != (s.SecretKey == ""):
		return fmt.Errorf("storage.s3.accessKey and secretKey must be set together, or both empty for an anonymous bucket")
	}
	if s.PresignTTL != "" {
		if _, err := time.ParseDuration(s.PresignTTL); err != nil {
			return fmt.Errorf("storage.s3.presignTTL: %w", err)
		}
	}
	return nil
}

type s3Store struct{ config StorageConfig }

func (c s3Store) s3() *S3Config { return c.config.S3 }

// anonymous reports an uncredentialed (public) bucket: requests are unsigned and
// urls are plain (no presign).
func (c s3Store) anonymous() bool {
	return !c.s3().HasCredentials()
}

func (c s3Store) ttl() time.Duration {
	if c.config.S3 != nil && c.config.S3.PresignTTL != "" {
		if d, err := time.ParseDuration(c.config.S3.PresignTTL); err == nil {
			return d
		}
	}
	return 15 * time.Minute
}

func (c s3Store) signer() sigV4 {
	return sigV4{accessKey: c.s3().AccessKey, secretKey: c.s3().SecretKey, region: c.s3().Region, service: "s3", now: time.Now}
}

// objectURL builds the object's url for a key, honoring path vs virtual-host
// addressing.
func (c s3Store) objectURL(key string) (*url.URL, error) {
	base, err := url.Parse(c.s3().Endpoint)
	if err != nil {
		return nil, err
	}
	if c.s3().PathStyle {
		base.Path = path.Join(base.Path, c.s3().Bucket, key)
		return base, nil
	}
	base.Host = c.s3().Bucket + "." + base.Host
	base.Path = "/" + key
	return base, nil
}

// key is a content-addressed object key.
func (c s3Store) key(media schema.MediaUpload) string {
	sum := sha256.Sum256(media.Data)
	name := hex.EncodeToString(sum[:8])
	if media.Name != nil && *media.Name != "" {
		name += "-" + path.Base(*media.Name)
	}
	return name
}

// Put uploads the media and returns a presigned GET url (so a private bucket is
// still fetchable); the reference expires with the presign TTL.
func (c s3Store) Put(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	u, err := c.objectURL(c.key(media))
	if err != nil {
		return schema.Reference{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(media.Data))
	if err != nil {
		return schema.Reference{}, err
	}
	if media.Mime != "" {
		req.Header.Set("Content-Type", media.Mime)
	}
	if !c.anonymous() {
		c.signer().signHeader(req, media.Data)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.Reference{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return schema.Reference{}, fmt.Errorf("storage: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if c.anonymous() {
		return schema.Reference{Source: "url", Value: u.String()}, nil
	}
	signed, err := c.signer().presign(http.MethodGet, u, c.ttl())
	if err != nil {
		return schema.Reference{}, err
	}
	expires := time.Now().Add(c.ttl())
	return schema.Reference{Source: "url", Value: signed, ExpiresAt: &expires}, nil
}

// sigV4 signs an HTTP request with AWS Signature Version 4 (header auth, and a
// presign variant). now is injectable for tests.
type sigV4 struct {
	accessKey, secretKey, region, service string
	now                                   func() time.Time
}

func (s sigV4) scope(date string) string {
	return strings.Join([]string{date, s.region, s.service, "aws4_request"}, "/")
}

func (s sigV4) signingKey(date string) []byte {
	k := hmacSHA256([]byte("AWS4"+s.secretKey), date)
	k = hmacSHA256(k, s.region)
	k = hmacSHA256(k, s.service)
	return hmacSHA256(k, "aws4_request")
}

// signHeader adds x-amz-date, x-amz-content-sha256 and Authorization for a
// header-authenticated request. body may be nil. Every request header (plus
// host) is signed, except a small skip list, so an extra header such as Range is
// covered.
func (s sigV4) signHeader(req *http.Request, body []byte) {
	amzDate, date := s.timestamps()
	payloadHash := sha256Hex(body)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	values := map[string]string{"host": host}
	for name, list := range req.Header {
		lower := strings.ToLower(name)
		if sigV4Skip[lower] {
			continue
		}
		values[lower] = strings.TrimSpace(strings.Join(list, ","))
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + values[name] + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		canonicalQuery(req.URL.Query()),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	signature := s.sign(canonicalRequest, amzDate, date)
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, s.scope(date), signedHeaders, signature))
}

// sigV4Skip are headers excluded from the signature (they are set or recomputed
// by intermediaries).
var sigV4Skip = map[string]bool{
	"authorization": true, "content-length": true, "user-agent": true,
	"accept-encoding": true, "connection": true,
}

// presign returns a presigned url for a method (query auth).
func (s sigV4) presign(method string, u *url.URL, ttl time.Duration) (string, error) {
	amzDate, date := s.timestamps()
	q := u.Query()
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", s.accessKey+"/"+s.scope(date))
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI(u),
		canonicalQuery(q),
		"host:" + u.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	signature := s.sign(canonicalRequest, amzDate, date)
	q.Set("X-Amz-Signature", signature)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s sigV4) sign(canonicalRequest, amzDate, date string) string {
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, s.scope(date), sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	return hex.EncodeToString(hmacSHA256(s.signingKey(date), stringToSign))
}

func (s sigV4) timestamps() (string, string) {
	now := s.now().UTC()
	return now.Format("20060102T150405Z"), now.Format("20060102")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// canonicalURI is the SigV4 canonical URI: the URI-encoded path. It starts from
// the decoded path, so a key that needs encoding is encoded exactly once (an
// already-escaped path would be double-encoded).
func canonicalURI(u *url.URL) string {
	return uriEncode(u.Path)
}

// uriEncode percent-encodes a path for the canonical request (slashes preserved,
// as S3 requires a single encoding of the object key).
func uriEncode(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if strings.IndexByte(unreserved, ch) >= 0 || ch == '/' {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

// canonicalQuery sorts query params and percent-encodes them per SigV4.
func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		vals := append([]string(nil), values[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			pairs = append(pairs, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(pairs, "&")
}
