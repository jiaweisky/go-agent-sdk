package config

import (
	"errors"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

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

// validate rejects a negative budget for any modality.
func (m MediaSizeLimit) validate(field string) error {
	if m.Default < 0 {
		return fmt.Errorf("media: %s.default must not be negative", field)
	}
	for _, limit := range []struct {
		name  string
		value int64
	}{
		{"image", m.Image},
		{"audio", m.Audio},
		{"video", m.Video},
		{"file", m.File},
	} {
		if limit.value < 0 {
			return fmt.Errorf("media: %s.%s must not be negative", field, limit.name)
		}
	}
	return nil
}

// MediaSpoolConfig is the last-resort spool location. {Root} expands to the
// server's state root, so a managed spool lives under it, never in the
// workspace.
type MediaSpoolConfig struct {
	Dir string `yaml:"dir"`
}

// MediaDeliveryConfig is the operator-owned inbound media delivery policy (the
// SDK's media: block). For each inbound attachment it decides how the bytes
// reach the model: inline (a native part, under inlineLimitBytes), as a url
// from the media store, as a provider file reference from mediaUpload, or
// spooled to disk; media above maxBytes is skipped. Prefer sets the attempt
// order, Upload enables the url/file sources, and Downsample shrinks an
// oversized payload first. This is delivery policy, not capability:
// models[].media declares what the model accepts.
type MediaDeliveryConfig struct {
	// InlineLimitBytes is the per-modality size budget (raw bytes).
	InlineLimitBytes MediaSizeLimit `yaml:"inlineLimitBytes"`
	// Prefer is the source order the policy attempts (file, url, inline); empty
	// means [file, inline].
	Prefer []string `yaml:"prefer"`
	// Upload allows adapting byte-backed media to a non-inline reference: a url
	// via the media store (storage) or a provider file reference via mediaUpload.
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

// Validate rejects a malformed media delivery policy at load: a negative size
// limit, an unknown prefer source, or a spool without a dir.
func (m *MediaDeliveryConfig) Validate() error {
	if m == nil {
		return nil
	}
	if err := m.InlineLimitBytes.validate("inlineLimitBytes"); err != nil {
		return err
	}
	if err := m.MaxBytes.validate("maxBytes"); err != nil {
		return err
	}
	for _, source := range m.Prefer {
		switch source {
		case "file", "url", "inline":
		default:
			return fmt.Errorf("media: unknown prefer source %q (file, url, inline)", source)
		}
	}
	if m.Spool != nil && m.Spool.Dir == "" {
		return errors.New("media: spool requires a dir")
	}
	return nil
}

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
	// ExpiresInSeconds, when > 0, bounds the SDK's local reference cache.
	ExpiresInSeconds int `yaml:"expiresInSeconds"`
	// ExpiresAfterSeconds, when > 0, is the provider file lifetime in seconds: it
	// is sent as the Files API `expires_after[anchor]=created_at` +
	// `expires_after[seconds]=N` (DeepSeek/OpenAI convention), so the provider
	// reclaims the file. It also bounds the local cache when ExpiresInSeconds is
	// unset. A value of 0 leaves the file permanent (the host owns cleanup).
	ExpiresAfterSeconds int `yaml:"expiresAfterSeconds"`
}

// Validate rejects a malformed uploader configuration.
func (c *MediaUploadConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.URL == "" {
		return errors.New("mediaUpload.url is required")
	}
	if (c.Header == "") != (c.Value == "") {
		return errors.New("mediaUpload.header and value must be set together")
	}
	if c.ExpiresInSeconds < 0 {
		return errors.New("mediaUpload.expiresInSeconds must not be negative")
	}
	if c.ExpiresAfterSeconds < 0 {
		return errors.New("mediaUpload.expiresAfterSeconds must not be negative")
	}
	return nil
}

var (
	// InputModalityNames are the model input modalities a host may declare.
	InputModalityNames = []string{"text", "image", "audio", "video", "pdf"}
	// MediaModalityNames are the media modalities a model's `media.forms` may key.
	MediaModalityNames = []string{"image", "audio", "video", "file"}
	// MediaFormNames are the wire forms a model may accept for a modality.
	MediaFormNames = []string{"inline", "url", "file"}
)

// ValidateInputModalities rejects unknown modality names.
func ValidateInputModalities(modalities []string) error {
	for _, modality := range modalities {
		if !slices.Contains(InputModalityNames, modality) {
			return fmt.Errorf("unknown input modality %q (text, image, audio, video, pdf)", modality)
		}
	}
	return nil
}

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
