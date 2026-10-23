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
	"strconv"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// NewUploader validates cfg and builds its provider Files API uploader. A nil
// config yields a nil uploader.
func NewUploader(cfg *config.MediaUploadConfig) (schema.MediaUploader, error) {
	if cfg == nil {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return uploadClient{config: cfg}, nil
}

type uploadClient struct{ config *config.MediaUploadConfig }

// Upload posts the media as multipart/form-data and returns the file reference.
func (c uploadClient) Upload(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
	req, err := c.newRequest(ctx, media)
	if err != nil {
		return schema.Reference{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.Reference{}, err
	}
	defer resp.Body.Close()
	return c.decodeReference(resp)
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
	if c.config.Header != "" && c.config.Value != "" {
		req.Header.Set(c.config.Header, c.config.Value)
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
	part, err := writer.CreateFormFile("file", name)
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
	if c.config.ExpiresAfterSeconds > 0 {
		_ = writer.WriteField("expires_after[anchor]", "created_at")
		_ = writer.WriteField("expires_after[seconds]", strconv.Itoa(c.config.ExpiresAfterSeconds))
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
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return schema.Reference{}, err
	}
	idField := c.config.IDField
	if idField == "" {
		idField = "id"
	}
	id, _ := decoded[idField].(string)
	if id == "" {
		return schema.Reference{}, fmt.Errorf("media upload: response has no %q", idField)
	}
	ref := schema.Reference{Source: "file", Value: id}
	if lifetime := c.referenceLifetime(); lifetime > 0 {
		expires := time.Now().Add(time.Duration(lifetime) * time.Second)
		ref.ExpiresAt = &expires
	}
	return ref, nil
}

// referenceLifetime is the local cache bound: ExpiresInSeconds, or ExpiresAfterSeconds when
// ExpiresInSeconds is unset.
func (c uploadClient) referenceLifetime() int {
	if c.config.ExpiresInSeconds != 0 {
		return c.config.ExpiresInSeconds
	}
	return c.config.ExpiresAfterSeconds
}
