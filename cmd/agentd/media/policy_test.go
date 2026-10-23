package media

import (
	"testing"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestDeliveryPolicyNil(t *testing.T) {
	var cfg *config.MediaDeliveryConfig
	if DeliveryPolicy(cfg, "/root") != nil {
		t.Fatal("nil config should yield a nil policy")
	}
}

func TestDeliveryPolicyExpandsRoot(t *testing.T) {
	cfg := &config.MediaDeliveryConfig{
		InlineLimitBytes: config.MediaSizeLimit{Default: 262144},
		Prefer:           []string{"file", "inline"},
		Upload:           true,
		Downsample:       true,
		Spool:            &config.MediaSpoolConfig{Dir: "{Root}/data/spool"},
		MaxBytes:         config.MediaSizeLimit{Default: 52428800},
	}
	policy := DeliveryPolicy(cfg, "/state/srv")
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
