package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// httpStore writes objects with an HTTP PUT under the configured base URL.
type httpStore struct{ config *config.StorageConfig }

// authHeaders is the configured credential: the Auth scheme (basic/bearer) and,
// when set, the custom Header/Value. Both reads and writes use it.
func (c httpStore) authHeaders() map[string]string {
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
func (c httpStore) base() string {
	return strings.TrimRight(c.config.URL, "/") + "/"
}

// ttl bounds a presigned url's lifetime (default 15m).
func (c httpStore) ttl() time.Duration {
	if c.config.PresignTTL != "" {
		if d, err := time.ParseDuration(c.config.PresignTTL); err == nil {
			return d
		}
	}

	return 15 * time.Minute
}

// presignURL signs an object url with nginx's secure_link scheme (md5+expires).
func (c httpStore) presignURL(objectURL string, expires time.Time) (string, error) {
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
func (c httpStore) Put(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	if c.config.URL == "" {
		return schema.Reference{}, errors.New("storage: url is required")
	}

	name := objectName(media)
	objectURL := c.base() + name

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, bytes.NewReader(media.Data))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return schema.Reference{}, fmt.Errorf("storage: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if c.config.PresignSecret == "" {
		return schema.Reference{Source: "url", Value: objectURL}, nil
	}

	expires := time.Now().Add(c.ttl())
	signed, err := c.presignURL(objectURL, expires)
	if err != nil {
		return schema.Reference{}, err
	}

	return schema.Reference{Source: "url", Value: signed, ExpiresAt: &expires}, nil
}

// secureLinkMD5 computes nginx secure_link's base64url(md5("<expires><uri> <secret>")).
func secureLinkMD5(uri string, expires int64, secret string) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%d%s %s", expires, uri, secret)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
