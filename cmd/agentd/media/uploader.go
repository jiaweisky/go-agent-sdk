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
	if c.config.URL == "" {
		return schema.Reference{}, errors.New("media upload: url is required")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	name := "file"
	if media.Name != nil && *media.Name != "" {
		name = *media.Name
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return schema.Reference{}, err
	}
	if _, err := part.Write(media.Data); err != nil {
		return schema.Reference{}, err
	}
	for key, value := range c.config.Fields {
		if err := writer.WriteField(key, value); err != nil {
			return schema.Reference{}, err
		}
	}
	if c.config.ExpiresAfter > 0 {
		_ = writer.WriteField("expires_after[anchor]", "created_at")
		_ = writer.WriteField("expires_after[seconds]", strconv.Itoa(c.config.ExpiresAfter))
	}
	if err := writer.Close(); err != nil {
		return schema.Reference{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, &buf)
	if err != nil {
		return schema.Reference{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.config.Header != "" && c.config.Value != "" {
		req.Header.Set(c.config.Header, c.config.Value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.Reference{}, err
	}
	defer resp.Body.Close()
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
	lifetime := c.config.ExpiresIn
	if lifetime == 0 {
		lifetime = c.config.ExpiresAfter
	}
	if lifetime > 0 {
		expires := time.Now().Add(time.Duration(lifetime) * time.Second)
		ref.ExpiresAt = &expires
	}
	return ref, nil
}
