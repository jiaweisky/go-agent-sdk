package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"
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

// UnmarshalYAML accepts a scalar (all modalities) or a map (per modality) and
// rejects unknown map keys, which a plain Node.Decode would silently ignore.
func (m *MediaSizeLimit) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var value int64
		if err := node.Decode(&value); err != nil {
			return err
		}
		*m = MediaSizeLimit{Default: value}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("media size limit must be a number or a mapping")
	}
	var out MediaSizeLimit
	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if _, dup := seen[key.Value]; dup {
			return fmt.Errorf("media size limit: duplicate key %q", key.Value)
		}
		seen[key.Value] = struct{}{}
		var limit int64
		if err := value.Decode(&limit); err != nil {
			return fmt.Errorf("media size limit %q: %w", key.Value, err)
		}
		switch key.Value {
		case "default":
			out.Default = limit
		case "image":
			out.Image = limit
		case "audio":
			out.Audio = limit
		case "video":
			out.Video = limit
		case "file":
			out.File = limit
		default:
			return fmt.Errorf("media size limit: unknown key %q", key.Value)
		}
	}
	*m = out
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

// ServerMediaConfig is a server's media policy: how bytes reach the model and
// where they may be uploaded.
type ServerMediaConfig struct {
	Delivery *MediaDeliveryConfig `yaml:"delivery"`
	Uploads  *MediaUploadsConfig  `yaml:"uploads"`
}

// MediaUploadsConfig groups the upload destinations.
type MediaUploadsConfig struct {
	// URLHost is the provider-independent bytes→URL store.
	URLHost *StorageConfig `yaml:"urlHost"`
}

// Validate checks the nested media policy.
func (m *ServerMediaConfig) Validate() error {
	if m == nil {
		return nil
	}
	if err := m.Delivery.Validate(); err != nil {
		return err
	}
	if m.Uploads != nil {
		if err := m.Uploads.URLHost.Validate(); err != nil {
			return fmt.Errorf("media.uploads.urlHost: %w", err)
		}
	}
	return nil
}

// MediaDeliveryConfig is the operator-owned inbound delivery policy. For each
// inbound attachment it decides whether bytes stay inline, are hosted as a URL,
// are uploaded to a provider Files API as a file_id, or are spooled to disk.
type MediaDeliveryConfig struct {
	// InlineLimitBytes is the per-modality size budget (raw bytes).
	InlineLimitBytes MediaSizeLimit `yaml:"inlineLimitBytes"`
	// Prefer is the source order (providerFile, url, inline); empty means
	// [providerFile, inline].
	Prefer []string `yaml:"prefer"`
	// AllowUploads enables non-inline hosting (url or provider file).
	AllowUploads bool `yaml:"allowUploads"`
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
// limit, an unknown prefer source, a duplicate source, or a spool without a dir.
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

	seen := map[string]bool{}
	for _, source := range m.Prefer {
		switch source {
		case "providerFile", "url", "inline":
		default:
			return fmt.Errorf("media.delivery.prefer: unknown source %q (providerFile, url, inline)", source)
		}

		if seen[source] {
			return fmt.Errorf("media.delivery.prefer: duplicate source %q", source)
		}

		seen[source] = true
	}

	if m.Spool != nil && m.Spool.Dir == "" {
		return errors.New("media.delivery.spool requires a dir")
	}

	return nil
}

// ProviderFileAPI is one reusable provider Files API configuration.
type ProviderFileAPI struct {
	// Kind is the upload transport shape. Only multipart-json is implemented.
	Kind string           `yaml:"kind"`
	URL  string           `yaml:"url"`
	Auth ProviderFileAuth `yaml:"auth"`
	// Fields are static multipart fields (for example purpose=user_data).
	Fields map[string]string `yaml:"fields"`
	// IDField is the JSON response key holding the file id; empty means "id".
	IDField string             `yaml:"idField"`
	Expiry  ProviderFileExpiry `yaml:"expiry"`
	MIME    ProviderFileMIME   `yaml:"mime"`
	Limits  ProviderFileLimits `yaml:"limits"`
	// ModelHeaders are applied to model requests that carry a file reference.
	ModelHeaders map[string]string `yaml:"modelHeaders"`
}

// ProviderFileAuth carries the upload request headers.
type ProviderFileAuth struct {
	Headers map[string]string `yaml:"headers"`
}

// ProviderFileExpiry selects the provider-side expiry field.
type ProviderFileExpiry struct {
	// Style is none, expires_after or expires_in_seconds.
	Style   string `yaml:"style"`
	Seconds int    `yaml:"seconds"`
}

// ProviderFileMIME is the provider Files API allow-list.
type ProviderFileMIME struct {
	Allowed []string `yaml:"allowed"`
}

// ProviderFileLimits bounds one upload.
type ProviderFileLimits struct {
	MaxBytes int64  `yaml:"maxBytes"`
	Timeout  string `yaml:"timeout"`
}

// validateProviderFileAPIs checks every named profile.
func validateProviderFileAPIs(profiles map[string]*ProviderFileAPI) error {
	for name, profile := range profiles {
		if strings.TrimSpace(name) == "" {
			return errors.New("providerFileAPIs: profile name must not be empty")
		}

		if err := profile.Validate(name); err != nil {
			return err
		}
	}

	return nil
}

// validateModelProviderFileAPIs checks model references before the registry is
// built.
func validateModelProviderFileAPIs(models []ModelConfig, profiles map[string]*ProviderFileAPI) error {
	for _, m := range models {
		if m.ProviderFileAPI == "" {
			continue
		}

		if m.ProviderID == "" {
			return fmt.Errorf("model %q: providerFileAPI %q requires an explicit providerID", m.ID, m.ProviderFileAPI)
		}

		if _, ok := profiles[m.ProviderFileAPI]; !ok {
			return fmt.Errorf("model %q: unknown providerFileAPI %q; known profiles: %s", m.ID, m.ProviderFileAPI, profileNames(profiles))
		}
	}

	return nil
}

func validateProviderHeaderName(profile, group, header, value string) error {
	if !httpguts.ValidHeaderFieldName(header) {
		return fmt.Errorf("providerFileAPIs.%s.%s: %q is not a valid header name", profile, group, header)
	}

	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("providerFileAPIs.%s.%s.%s must not be empty", profile, group, header)
	}

	return nil
}

func profileNames(profiles map[string]*ProviderFileAPI) string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ", ")
}

// TimeoutDuration parses Limits.Timeout; an empty value means the client default.
func (l ProviderFileLimits) TimeoutDuration() (time.Duration, error) {
	if l.Timeout == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(l.Timeout)
	if err != nil {
		return 0, fmt.Errorf("media provider files timeout: %w", err)
	}

	if d <= 0 {
		return 0, errors.New("media provider files timeout must be positive")
	}

	return d, nil
}

// Validate rejects a malformed provider Files API profile.
func (p *ProviderFileAPI) Validate(name string) error {
	if p == nil {
		return fmt.Errorf("providerFileAPIs.%s: profile is nil", name)
	}

	if p.Kind != "multipart-json" {
		return fmt.Errorf("providerFileAPIs.%s.kind must be multipart-json, got %q", name, p.Kind)
	}

	parsed, err := url.Parse(p.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("providerFileAPIs.%s.url must be an absolute http(s) URL", name)
	}

	for header, value := range p.Auth.Headers {
		if err := validateProviderHeaderName(name, "auth.headers", header, value); err != nil {
			return err
		}
	}

	if len(p.Auth.Headers) == 0 {
		return fmt.Errorf("providerFileAPIs.%s.auth.headers is required", name)
	}

	for header, value := range p.ModelHeaders {
		if err := validateProviderHeaderName(name, "modelHeaders", header, value); err != nil {
			return err
		}
	}

	switch p.Expiry.Style {
	case "none":
		if p.Expiry.Seconds != 0 {
			return fmt.Errorf("providerFileAPIs.%s.expiry.seconds requires expires_after or expires_in_seconds", name)
		}
	case "expires_after", "expires_in_seconds":
		if p.Expiry.Seconds <= 0 {
			return fmt.Errorf("providerFileAPIs.%s.expiry.seconds must be positive", name)
		}
	default:
		return fmt.Errorf("providerFileAPIs.%s.expiry.style must be none, expires_after or expires_in_seconds, got %q", name, p.Expiry.Style)
	}

	if len(p.MIME.Allowed) == 0 {
		return fmt.Errorf("providerFileAPIs.%s.mime.allowed must not be empty", name)
	}

	for _, mime := range p.MIME.Allowed {
		if mime != strings.TrimSpace(mime) || !strings.Contains(mime, "/") || mime != strings.ToLower(mime) {
			return fmt.Errorf("providerFileAPIs.%s.mime.allowed entry %q must be a lower-case MIME type without spaces", name, mime)
		}
	}

	if p.Limits.MaxBytes <= 0 {
		return fmt.Errorf("providerFileAPIs.%s.limits.maxBytes must be positive", name)
	}

	if _, err := p.Limits.TimeoutDuration(); err != nil {
		return fmt.Errorf("providerFileAPIs.%s.limits: %w", name, err)
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
