package config

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestInboundMediaPolicyNil(t *testing.T) {
	var m *InboundMediaConfig
	if m.Policy("/root") != nil {
		t.Fatal("nil config should yield a nil policy")
	}
}

func TestInboundMediaPolicyExpandsRoot(t *testing.T) {
	m := &InboundMediaConfig{
		InlineLimitBytes: MediaSizeLimit{Default: 262144},
		Prefer:           []string{"file", "inline"},
		Upload:           true,
		Downsample:       true,
		Spool:            &MediaSpoolConfig{Dir: "{Root}/data/spool"},
		MaxBytes:         MediaSizeLimit{Default: 52428800},
	}
	policy := m.Policy("/state/srv")
	if policy == nil {
		t.Fatal("policy should not be nil")
	}
	if policy.Spool == nil || policy.Spool.Dir != "/state/srv/data/spool" {
		t.Fatalf("spool = %+v, want /state/srv/data/spool", policy.Spool)
	}
	if len(policy.Prefer) != 2 || policy.Prefer[0] != core.MediaPolicySourceFile || policy.Prefer[1] != core.MediaPolicySourceInline {
		t.Fatalf("prefer = %v", policy.Prefer)
	}
	if policy.InlineLimitBytes.For("image") != 262144 || policy.MaxBytes.For("video") != 52428800 || !policy.Upload || !policy.Downsample {
		t.Fatalf("policy = %+v", policy)
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("converted policy should validate: %v", err)
	}
}

func TestValidateInboundMedia(t *testing.T) {
	if err := ValidateInboundMedia(nil); err != nil {
		t.Fatalf("nil media should be valid: %v", err)
	}
	if err := ValidateInboundMedia(&InboundMediaConfig{Prefer: []string{"url", "file", "inline"}}); err != nil {
		t.Fatalf("url is a valid prefer source: %v", err)
	}
	if err := ValidateInboundMedia(&InboundMediaConfig{Prefer: []string{"bogus"}}); err == nil {
		t.Fatal("an unknown prefer source must be rejected")
	}
	if err := ValidateInboundMedia(&InboundMediaConfig{Spool: &MediaSpoolConfig{}}); err == nil {
		t.Fatal("spool without a dir must be rejected")
	}
	if err := ValidateInboundMedia(&InboundMediaConfig{InlineLimitBytes: MediaSizeLimit{Default: -1}}); err == nil {
		t.Fatal("a negative inline limit must be rejected")
	}
	if err := ValidateInboundMedia(&InboundMediaConfig{Prefer: []string{"file"}, Spool: &MediaSpoolConfig{Dir: "{Root}/data/spool"}}); err != nil {
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
	if cfg.Inline.limits().For("image") != 262144 || cfg.Inline.limits().For("video") != 262144 {
		t.Fatalf("scalar limit = %+v (want 262144 for every modality)", cfg.Inline)
	}
	if cfg.Max.limits().For("image") != 33554432 || cfg.Max.limits().For("video") != 268435456 || cfg.Max.limits().For("audio") != 67108864 {
		t.Fatalf("per-modality limit = %+v", cfg.Max)
	}
}
