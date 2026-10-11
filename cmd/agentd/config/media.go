package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

// MediaUploadConfig configures a generic provider Files API uploader: a
// multipart POST with a "file" part (plus any extra fields) whose JSON response
// names the stored file. Provider lifecycle (list/delete/expiry) stays with the
// host.
type MediaUploadConfig struct {
	URL     string            `yaml:"url"`
	Header  string            `yaml:"header"`
	Value   string            `yaml:"value"`
	Fields  map[string]string `yaml:"fields"`
	IDField string            `yaml:"idField"`
	// ExpiresIn, when > 0, is the reference lifetime in seconds.
	ExpiresIn int `yaml:"expiresIn"`
}

// Uploader builds an SDK MediaUploader from the config.
func (c MediaUploadConfig) Uploader() schema.MediaUploader {
	return mediaUploadClient{config: c}
}

type mediaUploadClient struct{ config MediaUploadConfig }

// Upload posts the media as multipart/form-data and returns the file reference.
func (c mediaUploadClient) Upload(ctx context.Context, media schema.MediaUpload) (schema.MediaRef, error) {
	if c.config.URL == "" {
		return schema.MediaRef{}, errors.New("media upload: url is required")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	name := "file"
	if media.Name != nil && *media.Name != "" {
		name = *media.Name
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return schema.MediaRef{}, err
	}
	if _, err := part.Write(media.Data); err != nil {
		return schema.MediaRef{}, err
	}
	for key, value := range c.config.Fields {
		if err := writer.WriteField(key, value); err != nil {
			return schema.MediaRef{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return schema.MediaRef{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, &buf)
	if err != nil {
		return schema.MediaRef{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.config.Header != "" && c.config.Value != "" {
		req.Header.Set(c.config.Header, c.config.Value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.MediaRef{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return schema.MediaRef{}, fmt.Errorf("media upload: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return schema.MediaRef{}, err
	}
	idField := c.config.IDField
	if idField == "" {
		idField = "id"
	}
	id, _ := decoded[idField].(string)
	if id == "" {
		return schema.MediaRef{}, fmt.Errorf("media upload: response has no %q", idField)
	}
	ref := schema.MediaRef{Source: "file", ID: id}
	if c.config.ExpiresIn > 0 {
		expires := time.Now().Add(time.Duration(c.config.ExpiresIn) * time.Second)
		ref.ExpiresAt = &expires
	}
	return ref, nil
}

// InputModalityNames are the model input modalities a host may declare.
var InputModalityNames = []string{"text", "image", "audio", "video", "pdf"}

// ValidateInputModalities rejects unknown modality names.
func ValidateInputModalities(modalities []string) error {
	for _, modality := range modalities {
		if !slices.Contains(InputModalityNames, modality) {
			return fmt.Errorf("unknown input modality %q (text, image, audio, video, pdf)", modality)
		}
	}
	return nil
}

// MediaSourceNames are the media sources a model may declare.
var MediaSourceNames = []string{"inline", "url", "file"}

// ValidateMediaSources rejects unknown media source names.
func ValidateMediaSources(sources []string) error {
	for _, source := range sources {
		if !slices.Contains(MediaSourceNames, source) {
			return fmt.Errorf("unknown media source %q (inline, url, file)", source)
		}
	}
	return nil
}
