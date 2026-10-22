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
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
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
	// ExpiresIn, when > 0, bounds the SDK's local reference cache (seconds).
	ExpiresIn int `yaml:"expiresIn"`
	// ExpiresAfter, when > 0, is the provider file lifetime in seconds: it is
	// sent as the Files API `expires_after[anchor]=created_at` +
	// `expires_after[seconds]=N` (DeepSeek/OpenAI convention), so the provider
	// reclaims the file. It also bounds the local cache when ExpiresIn is unset.
	// A value of 0 leaves the file permanent (the host owns cleanup).
	ExpiresAfter int `yaml:"expiresAfter"`
}

// InboundMediaConfig is the operator-owned inbound media delivery policy (the
// SDK's media: block). It decides, per inbound attachment, whether media reaches
// the model inline, as a provider file reference, or spooled on disk. It is the
// delivery policy, distinct from a model's media capability (models[].media =
// input/sources).
type InboundMediaConfig struct {
	// InlineLimitBytes is the per-modality size budget (raw bytes).
	InlineLimitBytes MediaSizeLimit `yaml:"inlineLimitBytes"`
	// Prefer is the source order the policy attempts (file, url, inline); empty
	// means [file, inline].
	Prefer []string `yaml:"prefer"`
	// Upload allows adapting byte-backed media to a provider file reference via
	// mediaUpload.
	Upload bool `yaml:"upload"`
	// Downsample shrinks a byte-backed payload over InlineLimitBytes before a
	// source is chosen (independent of prefer).
	Downsample bool `yaml:"downsample"`
	// Spool is the last-resort disk location for undeliverable byte-backed media.
	Spool *MediaSpoolConfig `yaml:"spool"`
	// MaxBytes is the per-modality hard ceiling; larger byte-backed media is
	// skipped (noted).
	MaxBytes MediaSizeLimit `yaml:"maxBytes"`
}

// MediaSizeLimit is a per-modality byte budget (raw bytes). It accepts either a
// bare number (the default for every modality) or a map with a `default` plus
// per-modality overrides (image/audio/video/file). Zero means no limit.
type MediaSizeLimit struct {
	Default int64 `yaml:"default"`
	Image   int64 `yaml:"image"`
	Audio   int64 `yaml:"audio"`
	Video   int64 `yaml:"video"`
	File    int64 `yaml:"file"`
}

// UnmarshalYAML accepts a scalar (all modalities) or a map (per modality).
func (m *MediaSizeLimit) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var value int64
		if err := node.Decode(&value); err != nil {
			return err
		}
		*m = MediaSizeLimit{Default: value}
		return nil
	}
	type raw MediaSizeLimit
	var decoded raw
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*m = MediaSizeLimit(decoded)
	return nil
}

// limits converts the config to the SDK type.
func (m MediaSizeLimit) limits() core.MediaSizeLimits {
	by := map[string]int64{}
	for name, value := range map[string]int64{"image": m.Image, "audio": m.Audio, "video": m.Video, "file": m.File} {
		if value > 0 {
			by[name] = value
		}
	}
	if len(by) == 0 {
		by = nil
	}
	return core.MediaSizeLimits{Default: m.Default, ByModality: by}
}

// MediaSpoolConfig is the last-resort spool location. {Root} expands to the
// server's state root, so a managed spool lives under it, never in the
// workspace.
type MediaSpoolConfig struct {
	Dir string `yaml:"dir"`
}

// Policy converts the config to the SDK policy, expanding {Root} in spool.dir to
// root. A nil config yields nil (the SDK keeps the caller-declared behavior).
func (c *InboundMediaConfig) Policy(root string) *core.MediaPolicy {
	if c == nil {
		return nil
	}
	prefer := make([]core.MediaPolicySource, 0, len(c.Prefer))
	for _, name := range c.Prefer {
		prefer = append(prefer, core.MediaPolicySource(name))
	}
	var spool *core.MediaSpoolPolicy
	if c.Spool != nil {
		spool = &core.MediaSpoolPolicy{Dir: strings.ReplaceAll(c.Spool.Dir, "{Root}", root)}
	}
	return &core.MediaPolicy{
		InlineLimitBytes: c.InlineLimitBytes.limits(),
		Prefer:           prefer,
		Upload:           c.Upload,
		Downsample:       c.Downsample,
		Spool:            spool,
		MaxBytes:         c.MaxBytes.limits(),
	}
}

// ValidateInboundMedia rejects a malformed media delivery policy at load.
func ValidateInboundMedia(m *InboundMediaConfig) error {
	if m == nil {
		return nil
	}
	return m.Policy("").Validate()
}

// Uploader builds an SDK MediaUploader from the config.
func (c MediaUploadConfig) Uploader() schema.MediaUploader {
	return mediaUploadClient{config: c}
}

type mediaUploadClient struct{ config MediaUploadConfig }

// Upload posts the media as multipart/form-data and returns the file reference.
func (c mediaUploadClient) Upload(ctx context.Context, media schema.MediaUpload) (schema.Reference, error) {
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

// MediaModalityNames are the media modalities a model's `media.forms` may key.
var MediaModalityNames = []string{"image", "audio", "video", "file"}

// MediaFormNames are the wire forms a model may accept for a modality.
var MediaFormNames = []string{"inline", "url", "file"}

// ValidateMediaForms rejects unknown modality keys or form names in a model's
// per-modality `media.forms` map.
func ValidateMediaForms(forms map[string][]string) error {
	for modality, list := range forms {
		if !slices.Contains(MediaModalityNames, modality) {
			return fmt.Errorf("unknown media modality %q (image, audio, video, file)", modality)
		}
		for _, form := range list {
			if !slices.Contains(MediaFormNames, form) {
				return fmt.Errorf("media %s: unknown form %q (inline, url, file)", modality, form)
			}
		}
	}
	return nil
}
