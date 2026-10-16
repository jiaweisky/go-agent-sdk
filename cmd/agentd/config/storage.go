package config

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// StorageConfig configures the process's provider-independent media store: it
// hosts bytes and returns an http(s) url a provider can fetch (and, with a
// storage extension, a peer can write to). One store serves every provider.
type StorageConfig struct {
	// Backend names the storage mechanism: "http" (a PUT to URL), "local" (a
	// shared directory), "s3" (S3-compatible) or "cos" (native Tencent COS).
	Backend string `yaml:"backend"`
	// URL is the http backend's upload base; a content-addressed object is
	// written under it.
	URL string `yaml:"url"`
	// Auth is the http backend's credential scheme: "" / "none", "basic" or
	// "bearer". It is sent on both writes (PUT) and reads (GET).
	Auth string `yaml:"auth"`
	// Username / Password authenticate "basic".
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// Token authenticates "bearer".
	Token string `yaml:"token"`
	// Header/Value, when both set, add a custom auth header instead (or on top)
	// of Auth — e.g. a non-standard "X-Api-Key".
	Header string `yaml:"header"`
	Value  string `yaml:"value"`
	// PresignSecret, when set, signs http urls with nginx's secure_link scheme
	// (md5 + expires), so a private origin can accept a provider's fetches (and
	// a broker PUT slot) without a shared header credential.
	PresignSecret string `yaml:"presignSecret"`
	// PresignURL, when set, delegates signing to an external service instead: a
	// POST {key, method, expires} returns {uploadUrl, reference}. It is mutually
	// exclusive with PresignSecret, and covers upload slots only.
	PresignURL string `yaml:"presignURL"`
	// PresignTTL bounds a presigned url's lifetime. Empty defaults to 15m.
	PresignTTL string `yaml:"presignTTL"`
	// Root is the local backend's shared directory.
	Root string `yaml:"root"`
	// ServeBaseURL (local) is where Root is served over HTTP, so the url is
	// fetchable by a provider; empty leaves the object reachable only on disk.
	ServeBaseURL string `yaml:"serveBaseURL"`
	// S3 configures an S3-compatible object store (backend "s3").
	S3 *S3Config `yaml:"s3"`
	// COS configures a Tencent COS bucket over its native REST API (backend "cos").
	COS *COSConfig `yaml:"cos"`
	// MediaTypes allowed on A2A ingest (patterns such as "image/*").
	MediaTypes []string `yaml:"mediaTypes"`
	// MaxInlineBytes / MaxBytes bound A2A ingest (referenced files).
	MaxInlineBytes int64 `yaml:"maxInlineBytes"`
	MaxBytes       int64 `yaml:"maxBytes"`
}

// Store builds the SDK MediaStore from the config.
func (c StorageConfig) Store() schema.MediaStore {
	switch c.Backend {
	case "local":
		return localStore{config: c}
	case "s3":
		return s3Store{config: c}
	case "cos":
		return cosStore{config: c}
	default:
		return mediaStoreClient{config: c}
	}
}

// ValidateStorage rejects a malformed store configuration.
func ValidateStorage(s *StorageConfig) error {
	if s == nil {
		return nil
	}
	switch s.Backend {
	case "", "http":
		if s.URL == "" {
			return errors.New("storage.url is required")
		}
		if err := validateHTTPAuth(s); err != nil {
			return err
		}
	case "local":
		if s.Root == "" {
			return errors.New("storage.root is required")
		}
	case "s3":
		if s.S3 == nil {
			return errors.New("storage.s3 is required when backend is s3")
		}
		return ValidateS3(s.S3)
	case "cos":
		if s.COS == nil {
			return errors.New("storage.cos is required when backend is cos")
		}
		return ValidateCOS(s.COS)
	default:
		return fmt.Errorf("storage.backend must be http, local, s3 or cos, got %q", s.Backend)
	}
	return nil
}

// validateHTTPAuth rejects an inconsistent http auth configuration.
func validateHTTPAuth(s *StorageConfig) error {
	switch s.Auth {
	case "", "none":
	case "basic":
		if s.Username == "" || s.Password == "" {
			return errors.New("storage.auth basic requires username and password")
		}
	case "bearer":
		if s.Token == "" {
			return errors.New("storage.auth bearer requires token")
		}
	default:
		return fmt.Errorf("storage.auth must be none, basic or bearer, got %q", s.Auth)
	}
	if (s.Header == "") != (s.Value == "") {
		return errors.New("storage.header and storage.value must be set together")
	}
	if s.PresignSecret != "" && s.PresignURL != "" {
		return errors.New("storage.presignSecret and storage.presignURL are mutually exclusive")
	}
	return nil
}

type mediaStoreClient struct{ config StorageConfig }

// authHeaders is the configured credential: the Auth scheme (basic/bearer) and,
// when set, the custom Header/Value. Both reads and writes use it.
func (c mediaStoreClient) authHeaders() map[string]string {
	headers := map[string]string{}
	switch c.config.Auth {
	case "basic":
		pair := c.config.Username + ":" + c.config.Password
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(pair))
	case "bearer":
		headers["Authorization"] = "Bearer " + c.config.Token
	}
	if c.config.Header != "" && c.config.Value != "" {
		headers[c.config.Header] = c.config.Value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// base is the store's upload/read prefix (trailing slash).
func (c mediaStoreClient) base() string {
	return strings.TrimRight(c.config.URL, "/") + "/"
}

// ttl bounds a presigned url's lifetime (default 15m).
func (c mediaStoreClient) ttl() time.Duration {
	if c.config.PresignTTL != "" {
		if d, err := time.ParseDuration(c.config.PresignTTL); err == nil {
			return d
		}
	}
	return 15 * time.Minute
}

// presignURL signs an object url with nginx's secure_link scheme (md5+expires).
func (c mediaStoreClient) presignURL(objectURL string, expires time.Time) (string, error) {
	u, err := url.Parse(objectURL)
	if err != nil {
		return "", err
	}
	ts := expires.Unix()
	sig := secureLinkMD5(u.Path, ts, c.config.PresignSecret)
	q := u.Query()
	q.Set("md5", sig)
	q.Set("expires", strconv.FormatInt(ts, 10))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Put writes the media under the store's base and returns a "url" reference.
// The object name is content-addressed, so a repeated Put is idempotent. With a
// PresignSecret the url carries a secure_link ticket, so a private origin can
// serve it to a provider.
func (c mediaStoreClient) Put(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	if c.config.URL == "" {
		return schema.Reference{}, errors.New("storage: url is required")
	}
	sum := sha256.Sum256(media.Data)
	name := hex.EncodeToString(sum[:8])
	if media.Name != nil && *media.Name != "" {
		name += "-" + filepath.Base(*media.Name)
	}
	url := c.base() + name
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(media.Data))
	if err != nil {
		return schema.Reference{}, err
	}
	for key, value := range c.authHeaders() {
		req.Header.Set(key, value)
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
	if c.config.PresignSecret == "" {
		return schema.Reference{Source: "url", Value: url}, nil
	}
	expires := time.Now().Add(c.ttl())
	signed, err := c.presignURL(url, expires)
	if err != nil {
		return schema.Reference{}, err
	}
	return schema.Reference{Source: "url", Value: signed, ExpiresAt: &expires}, nil
}

// Get reads a Put object back. It rejects a reference that is not this store's.
func (c mediaStoreClient) Get(ctx context.Context, ref schema.Reference) (io.ReadCloser, error) {
	if ref.Source != "url" || !strings.HasPrefix(ref.Value, c.base()) {
		return nil, fmt.Errorf("%w: %q", schema.ErrUnsupportedReference, ref.Value)
	}
	u, err := url.Parse(ref.Value)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.Value, nil)
	if err != nil {
		return nil, err
	}
	// A secure_link url carries its own ticket (md5+expires); only add the
	// shared credential to a bare object url.
	if u.Query().Get("expires") == "" {
		for key, value := range c.authHeaders() {
			req.Header.Set(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("storage: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

// Target is the static upload target a peer writes objects to (reference mode).
func (c mediaStoreClient) Target() schema.UploadTarget {
	return schema.UploadTarget{URL: c.base(), Method: http.MethodPut, Headers: c.authHeaders()}
}

// Reserve issues a presigned PUT slot (broker mode) when a PresignSecret is
// configured; without one an http store advertises no per-file slot (its static
// target, or the server's ingest endpoint, is the write path).
func (c mediaStoreClient) Reserve(ctx context.Context, name, mime string, size int64) (schema.UploadTarget, error) {
	if c.config.PresignURL != "" {
		return c.reserveViaService(ctx, name, mime, size)
	}
	if c.config.PresignSecret == "" {
		return schema.UploadTarget{}, nil
	}
	key := sanitizeKey(name)
	if key == "" {
		key = hex.EncodeToString([]byte(name + ":" + mime))
	}
	object := c.base() + key
	expires := time.Now().Add(c.ttl())
	signed, err := c.presignURL(object, expires)
	if err != nil {
		return schema.UploadTarget{}, err
	}
	return schema.UploadTarget{URL: signed, Method: http.MethodPut, Reference: object, Expires: expires}, nil
}

// reserveViaService asks an external signer for a PUT slot (broker mode): it
// POSTs {key, method, expires} and reads back {uploadUrl, reference}.
func (c mediaStoreClient) reserveViaService(ctx context.Context, name, mime string, size int64) (schema.UploadTarget, error) {
	key := sanitizeKey(name)
	if key == "" {
		key = hex.EncodeToString([]byte(name + ":" + mime))
	}
	body, err := json.Marshal(map[string]any{
		"key": key, "method": http.MethodPut, "expires": int64(c.ttl().Seconds()),
	})
	if err != nil {
		return schema.UploadTarget{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.PresignURL, bytes.NewReader(body))
	if err != nil {
		return schema.UploadTarget{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range c.authHeaders() {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.UploadTarget{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return schema.UploadTarget{}, fmt.Errorf("storage: presign: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out struct {
		UploadURL string `json:"uploadUrl"`
		Reference string `json:"reference"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return schema.UploadTarget{}, err
	}
	if out.UploadURL == "" {
		return schema.UploadTarget{}, errors.New("storage: presign response has no uploadUrl")
	}
	if out.Reference == "" {
		out.Reference = out.UploadURL
	}
	return schema.UploadTarget{
		URL: out.UploadURL, Method: http.MethodPut, Reference: out.Reference,
		Expires: time.Now().Add(c.ttl()),
	}, nil
}

// secureLinkMD5 computes nginx secure_link's base64url(md5("<expires><uri> <secret>")).
func secureLinkMD5(uri string, expires int64, secret string) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%d%s %s", expires, uri, secret)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
