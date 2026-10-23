package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateMediaUpload(t *testing.T) {
	var nilConfig *MediaUploadConfig
	if err := nilConfig.Validate(); err != nil {
		t.Fatalf("nil uploader should be valid: %v", err)
	}
	if err := (&MediaUploadConfig{}).Validate(); err == nil {
		t.Fatal("uploader without a url must be rejected")
	}
	if err := (&MediaUploadConfig{URL: "https://up.example", Header: "X-K"}).Validate(); err == nil {
		t.Fatal("header without value must be rejected")
	}
	if err := (&MediaUploadConfig{URL: "https://up.example", ExpiresIn: -1}).Validate(); err == nil {
		t.Fatal("a negative expiresIn must be rejected")
	}
	if err := (&MediaUploadConfig{URL: "https://up.example", Header: "X-K", Value: "v"}).Validate(); err != nil {
		t.Fatalf("valid uploader rejected: %v", err)
	}
}

func TestValidateMediaDelivery(t *testing.T) {
	if err := (*MediaDeliveryConfig)(nil).Validate(); err != nil {
		t.Fatalf("nil media should be valid: %v", err)
	}
	if err := (&MediaDeliveryConfig{Prefer: []string{"url", "file", "inline"}}).Validate(); err != nil {
		t.Fatalf("url is a valid prefer source: %v", err)
	}
	if err := (&MediaDeliveryConfig{Prefer: []string{"bogus"}}).Validate(); err == nil {
		t.Fatal("an unknown prefer source must be rejected")
	}
	if err := (&MediaDeliveryConfig{Spool: &MediaSpoolConfig{}}).Validate(); err == nil {
		t.Fatal("spool without a dir must be rejected")
	}
	if err := (&MediaDeliveryConfig{InlineLimitBytes: MediaSizeLimit{Default: -1}}).Validate(); err == nil {
		t.Fatal("a negative inline limit must be rejected")
	}
	if err := (&MediaDeliveryConfig{InlineLimitBytes: MediaSizeLimit{Image: -1}}).Validate(); err == nil {
		t.Fatal("a negative per-modality inline limit must be rejected")
	}
	if err := (&MediaDeliveryConfig{MaxBytes: MediaSizeLimit{Video: -1}}).Validate(); err == nil {
		t.Fatal("a negative per-modality maxBytes limit must be rejected")
	}
	if err := (&MediaDeliveryConfig{Spool: &MediaSpoolConfig{Dir: "{Root}"}}).Validate(); err != nil {
		t.Fatalf("a {Root}-only spool dir should validate: %v", err)
	}
	if err := (&MediaDeliveryConfig{Prefer: []string{"file"}, Spool: &MediaSpoolConfig{Dir: "{Root}/data/spool"}}).Validate(); err != nil {
		t.Fatalf("a well-formed policy should validate: %v", err)
	}
}

func TestMediaSizeLimitUnmarshal(t *testing.T) {
	var cfg struct {
		Inline MediaSizeLimit `yaml:"inlineLimitBytes"`
		Max    MediaSizeLimit `yaml:"maxBytes"`
	}
	doc := "inlineLimitBytes: 262144\nmaxBytes:\n  default: 67108864\n  image: 33554432\n  video: 268435456\n"
	if err := yaml.Unmarshal([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Inline.Default != 262144 {
		t.Fatalf("scalar limit = %+v (want 262144 for every modality)", cfg.Inline)
	}
	if cfg.Max.Default != 67108864 || cfg.Max.Image != 33554432 || cfg.Max.Video != 268435456 {
		t.Fatalf("per-modality limit = %+v", cfg.Max)
	}
}
