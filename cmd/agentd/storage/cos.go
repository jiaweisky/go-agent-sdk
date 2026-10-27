package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// cosStore is a Tencent COS store over its native REST API (q-sign-algorithm=sha1),
// needing no S3 SDK. A Put returns a presigned GET url for the provider.
type cosStore struct {
	config *config.StorageConfig
	// now is injectable for tests; nil uses time.Now.
	now func() time.Time
}

func (c cosStore) cos() *config.COSConfig { return c.config.COS }

// anonymous reports an uncredentialed (public) bucket: requests are unsigned and
// urls are plain (no presign).
func (c cosStore) anonymous() bool {
	return !c.cos().HasCredentials()
}

func (c cosStore) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c cosStore) ttl() time.Duration {
	if c.cos() != nil && c.cos().PresignTTL != "" {
		if d, err := time.ParseDuration(c.cos().PresignTTL); err == nil {
			return d
		}
	}

	return 15 * time.Minute
}

// host resolves the request host: the configured endpoint, or the default
// virtual-host form <bucket>.cos.<region>.myqcloud.com.
func (c cosStore) host() string {
	ep := c.cos().Endpoint
	if ep == "" {
		return c.cos().Bucket + ".cos." + c.cos().Region + ".myqcloud.com"
	}

	if u, err := url.Parse(ep); err == nil && u.Host != "" {
		return u.Host
	}

	return ep
}

func (c cosStore) scheme() string {
	if u, err := url.Parse(c.cos().Endpoint); err == nil && u.Scheme != "" {
		return u.Scheme
	}
	return "https"
}

// objectURL builds the object's url for a key.
func (c cosStore) objectURL(key string) string {
	return c.scheme() + "://" + c.host() + "/" + uriEncode(key)
}

// Put uploads the media and returns a presigned GET url (so a private bucket is
// still fetchable); the reference expires with the presign TTL.
func (c cosStore) Put(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	key := objectName(media)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.objectURL(key), bytes.NewReader(media.Data))
	if err != nil {
		return schema.Reference{}, err
	}

	if media.Mime != "" {
		req.Header.Set("Content-Type", media.Mime)
	}

	if !c.anonymous() {
		c.authorize(req, key, http.MethodPut)
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
		return schema.Reference{Source: "url", Value: c.objectURL(key)}, nil
	}

	expires := c.clock().Add(c.ttl())

	return schema.Reference{Source: "url", Value: c.presign("get", key, expires), ExpiresAt: &expires}, nil
}

// authorize adds the native COS Authorization header, signing host and any
// request query parameters.
func (c cosStore) authorize(req *http.Request, key, method string) {
	keyTime, signature, paramList := c.signature(method, key, req.URL.Query(), c.clock().Add(time.Hour))
	auth := fmt.Sprintf(
		"q-sign-algorithm=sha1&q-ak=%s&q-sign-time=%s&q-key-time=%s&q-header-list=host&q-url-param-list=%s&q-signature=%s",
		c.cos().SecretID, keyTime, keyTime, paramList, signature)
	req.Header.Set("Authorization", auth)

	if c.cos().SessionToken != "" {
		req.Header.Set("x-cos-security-token", c.cos().SessionToken)
	}
}

// presign renders a query-signed URL (the q-sign-* components as parameters).
func (c cosStore) presign(method, key string, expires time.Time) string {
	keyTime, signature, _ := c.signature(method, key, nil, expires)

	q := url.Values{}
	q.Set("q-sign-algorithm", "sha1")
	q.Set("q-ak", c.cos().SecretID)
	q.Set("q-sign-time", keyTime)
	q.Set("q-key-time", keyTime)
	q.Set("q-header-list", "host")
	q.Set("q-url-param-list", "")
	q.Set("q-signature", signature)

	if c.cos().SessionToken != "" {
		// Temporary credentials carry the session token as a query parameter; it
		// is not part of the signed parameters.
		q.Set("x-cos-security-token", c.cos().SessionToken)
	}

	return c.objectURL(key) + "?" + q.Encode()
}

// signature computes the COS sign key, string to sign, and signature over the
// signed headers (host) and the given request query parameters. It returns the
// key time, the signature, and the signed URL parameter list.
func (c cosStore) signature(
	method, key string, params url.Values, end time.Time,
) (keyTime, signature, paramList string) {
	paramString, paramList := cosParameters(params)
	keyTime = fmt.Sprintf("%d;%d", c.clock().Unix(), end.Unix())
	signKey := hmacSHA1Hex([]byte(c.cos().SecretKey), keyTime)
	httpString := method + "\n/" + key + "\n" + paramString + "\n" + "host=" + c.host() + "\n"
	stringToSign := "sha1\n" + keyTime + "\n" + sha1Hex(httpString) + "\n"

	return keyTime, hmacSHA1Hex([]byte(signKey), stringToSign), paramList
}

// cosParameters renders a query as COS's HttpParameters string (lowercase keys,
// sorted, joined by &) and the matching q-url-param-list (semicolon-joined).
// The q-sign-* components and the session token are not part of the signature.
func cosParameters(q url.Values) (params, list string) {
	lower := make(map[string][]string, len(q))

	for k, vs := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "q-") || lk == "x-cos-security-token" {
			continue
		}
		lower[lk] = append(lower[lk], vs...)
	}

	keys := make([]string, 0, len(lower))
	for k := range lower {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	parts := make([]string, 0, len(keys))

	for _, k := range keys {
		for _, v := range lower[k] {
			parts = append(parts, k+"="+v)
		}
	}

	return strings.Join(parts, "&"), strings.Join(keys, ";")
}

func hmacSHA1Hex(key []byte, msg string) string {
	h := hmac.New(sha1.New, key)
	h.Write([]byte(msg))

	return hex.EncodeToString(h.Sum(nil))
}

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
