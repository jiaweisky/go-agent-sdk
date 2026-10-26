package media

import (
	"strings"

	"github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

// DeliveryPolicy converts the config to the SDK media policy, expanding {Root}
// in spool.dir to root. A nil config yields nil (the SDK keeps the
// caller-declared behavior).
func DeliveryPolicy(cfg *config.MediaDeliveryConfig, root string) *core.MediaPolicy {
	if cfg == nil {
		return nil
	}
	prefer := make([]core.MediaPolicySource, 0, len(cfg.Prefer))
	for _, name := range cfg.Prefer {
		if name == "providerFile" {
			name = "file"
		}
		prefer = append(prefer, core.MediaPolicySource(name))
	}
	var spool *core.MediaSpoolPolicy
	if cfg.Spool != nil {
		spool = &core.MediaSpoolPolicy{Dir: strings.ReplaceAll(cfg.Spool.Dir, "{Root}", root)}
	}
	return &core.MediaPolicy{
		InlineLimitBytes: sizeLimits(cfg.InlineLimitBytes),
		Prefer:           prefer,
		Upload:           cfg.AllowUploads,
		Downsample:       cfg.Downsample,
		Spool:            spool,
		MaxBytes:         sizeLimits(cfg.MaxBytes),
	}
}

// sizeLimits converts a per-modality budget to the SDK type.
func sizeLimits(m config.MediaSizeLimit) core.MediaSizeLimits {
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
