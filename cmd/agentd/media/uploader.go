package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

const (
	// uploadTimeout bounds one provider upload attempt.
	uploadTimeout = 2 * time.Minute
	// maxUploadResponseBytes bounds the provider's JSON response.
	maxUploadResponseBytes = 1 << 20
)

// uploadClient is a generic Files API client driven only by configuration.
type uploadClient struct {
	config *config.ProviderFileAPI
	client *http.Client
}

// NewUploader validates cfg and builds its provider Files API uploader. A nil
// config yields a nil uploader.
func NewUploader(name string, cfg *config.ProviderFileAPI) (schema.MediaUploader, error) {
	if cfg == nil {
		return nil, nil
	}
	if err := cfg.Validate(name); err != nil {
		return nil, err
	}
	timeout := uploadTimeout
	if cfg.Limits.Timeout != "" {
		d, err := cfg.Limits.TimeoutDuration()
		if err != nil {
			return nil, err
		}
		timeout = d
	}
	return uploadClient{config: cfg, client: &http.Client{Timeout: timeout}}, nil
}

// MediaCapabilities reports the profile's provider-side MIME and size limits.
func (c uploadClient) MediaCapabilities() schema.MediaCapabilities {
	return schema.MediaCapabilities{MIMEs: c.config.MIME.Allowed, MaxBytes: c.config.Limits.MaxBytes}
}

// Upload posts the media as multipart/form-data and returns the file reference.
func (c uploadClient) Upload(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	if max := c.config.Limits.MaxBytes; max > 0 && int64(len(media.Data)) > max {
		return schema.Reference{}, fmt.Errorf("media upload %s: %d bytes exceeds maxBytes %d", safeURL(c.config.URL), len(media.Data), max)
	}
	req, err := c.newRequest(ctx, media)
	if err != nil {
		return schema.Reference{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return schema.Reference{}, fmt.Errorf("media upload %s: %w", safeURL(c.config.URL), err)
	}
	defer resp.Body.Close()
	ref, err := c.decodeReference(resp)
	if err != nil {
		return schema.Reference{}, fmt.Errorf("media upload %s: %w", safeURL(c.config.URL), err)
	}
	return ref, nil
}

// newRequest builds the multipart upload request.
func (c uploadClient) newRequest(ctx context.Context, media schema.MediaUpload) (*http.Request, error) {
	if c.config.URL == "" {
		return nil, errors.New("media upload: url is required")
	}
	body, contentType, err := c.multipartBody(media)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	for key, value := range c.config.Auth.Headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

// multipartBody renders the file part, configured fields and optional
// provider-side expiry.
func (c uploadClient) multipartBody(media schema.MediaUpload) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	name := "file"
	if media.Name != nil && *media.Name != "" {
		name = *media.Name
	}
	contentType := media.Mime
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition("file", name))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(media.Data); err != nil {
		return nil, "", err
	}

	for key, value := range c.config.Fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}

	switch c.config.Expiry.Style {
	case "expires_after":
		if err := writer.WriteField("expires_after[anchor]", "created_at"); err != nil {
			return nil, "", err
		}
		if err := writer.WriteField("expires_after[seconds]", strconv.Itoa(c.config.Expiry.Seconds)); err != nil {
			return nil, "", err
		}
	case "expires_in_seconds":
		if err := writer.WriteField("expires_in_seconds", strconv.Itoa(c.config.Expiry.Seconds)); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}

// decodeReference validates the response and reads the provider's file id.
func (c uploadClient) decodeReference(resp *http.Response) (schema.Reference, error) {
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return schema.Reference{}, fmt.Errorf("media upload: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var decoded map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxUploadResponseBytes)).Decode(&decoded); err != nil {
		return schema.Reference{}, err
	}
	idField := c.config.IDField
	if idField == "" {
		idField = "id"
	}
	id, ok := lookupString(decoded, idField)
	if !ok || id == "" {
		return schema.Reference{}, fmt.Errorf("media upload: response has no %q", idField)
	}
	ref := schema.Reference{Source: "file", Value: id}
	if lifetime := c.referenceLifetime(); lifetime > 0 {
		expires := time.Now().Add(time.Duration(lifetime) * time.Second)
		ref.ExpiresAt = &expires
	}
	return ref, nil
}

// safeURL strips userinfo so an error cannot leak credentials embedded in the
// profile URL.
func safeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

// lookupString reads a string at a dotted path (for example "data.id").
func lookupString(value map[string]any, path string) (string, bool) {
	var current any = value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[part]
		if !ok {
			return "", false
		}
	}
	text, ok := current.(string)
	return text, ok
}

// referenceLifetime is the provider file lifetime for the local reference
// cache; zero means the provider file is permanent.
func (c uploadClient) referenceLifetime() int {
	if c.config.Expiry.Style == "none" || c.config.Expiry.Style == "" {
		return 0
	}
	return c.config.Expiry.Seconds
}
