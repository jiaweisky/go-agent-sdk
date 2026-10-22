package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// MediaPolicySource names an inbound media source the delivery policy may pick.
type MediaPolicySource string

const (
	// MediaPolicySourceFile is a provider file reference (its Files API).
	MediaPolicySourceFile MediaPolicySource = "file"
	// MediaPolicySourceURL is an http(s) url the provider fetches, produced by the
	// provider-independent MediaStore (or supplied by the caller).
	MediaPolicySourceURL MediaPolicySource = "url"
	// MediaPolicySourceInline is an inline payload (bytes / data URL).
	MediaPolicySourceInline MediaPolicySource = "inline"
)

// MediaPolicySources lists the sources a policy may prefer.
var MediaPolicySources = []MediaPolicySource{MediaPolicySourceFile, MediaPolicySourceURL, MediaPolicySourceInline}

// MediaSpoolPolicy holds a last-resort spooled attachment's settings.
type MediaSpoolPolicy struct {
	// Dir is the managed directory spooled attachments are written to. It must
	// be an SDK state directory, never the workspace, so the host's read tool can
	// reach it and periodic cleanup can bound it.
	Dir string
}

// MediaSizeLimits is a per-modality byte budget. A modality with no explicit
// entry uses Default; a value of zero means "no limit".
type MediaSizeLimits struct {
	// Default applies to any modality without an explicit entry.
	Default int64 `json:"default,omitempty"`
	// ByModality overrides Default for a modality (image|audio|video|file).
	ByModality map[string]int64 `json:"byModality,omitempty"`
}

// For returns the byte limit for a modality (0 = no limit).
func (l MediaSizeLimits) For(modality string) int64 {
	if value, ok := l.ByModality[modality]; ok {
		return value
	}
	return l.Default
}

// IsZero reports whether no limit is configured at all.
func (l MediaSizeLimits) IsZero() bool {
	return l.Default == 0 && len(l.ByModality) == 0
}

// Validate rejects a negative budget.
func (l MediaSizeLimits) Validate(field string) error {
	if l.Default < 0 {
		return fmt.Errorf("media: %s.default must not be negative", field)
	}
	for modality, value := range l.ByModality {
		if value < 0 {
			return fmt.Errorf("media: %s.%s must not be negative", field, modality)
		}
	}
	return nil
}

// MediaPolicy is the operator-owned inbound media delivery policy. When set on
// ClientOptions it decides, per inbound attachment, whether media reaches the
// model inline, as an http(s) url, as a provider file reference, or (last
// resort) spooled on disk. A caller-declared reference still wins when feasible.
// A nil policy keeps the caller-declared behavior (inline-first).
type MediaPolicy struct {
	// InlineLimitBytes is the per-modality size budget (raw bytes): a payload
	// larger than its modality's budget cannot be inlined, and is shrunk first
	// when Downsample is set. Zero lifts the budget.
	InlineLimitBytes MediaSizeLimits
	// Prefer is the order of sources the policy attempts (file, url, inline).
	// Empty means [file, inline]. "url" is produced by the provider-independent
	// MediaStore; "file" by the provider's Files API.
	Prefer []MediaPolicySource
	// Upload allows adapting byte-backed media to a non-inline reference (a url
	// via the store, or a provider file reference via the uploader).
	Upload bool
	// Downsample shrinks a byte-backed payload that exceeds the inline budget
	// before a source is chosen, so the budget holds for both a file upload and
	// an inline part. It is independent of Prefer.
	Downsample bool
	// Spool, when set, is the last-resort disk location for byte-backed media the
	// model cannot take inline or as a file.
	Spool *MediaSpoolPolicy
	// MaxBytes is the per-modality hard ceiling: byte-backed media larger than
	// its modality's ceiling is skipped (with a note) instead of delivered. Zero
	// lifts it.
	MaxBytes MediaSizeLimits
}

// PreferredSources returns the policy's source order, defaulting to
// [file, inline].
func (p MediaPolicy) PreferredSources() []MediaPolicySource {
	if len(p.Prefer) == 0 {
		return []MediaPolicySource{MediaPolicySourceFile, MediaPolicySourceInline}
	}
	return p.Prefer
}

// Validate rejects a malformed policy.
func (p MediaPolicy) Validate() error {
	if err := p.InlineLimitBytes.Validate("inlineLimitBytes"); err != nil {
		return err
	}
	if err := p.MaxBytes.Validate("maxBytes"); err != nil {
		return err
	}
	for _, source := range p.Prefer {
		if !slices.Contains(MediaPolicySources, source) {
			return fmt.Errorf("media: unknown prefer source %q (file, url, inline)", source)
		}
	}
	if p.Spool != nil && p.Spool.Dir == "" {
		return fmt.Errorf("media: spool requires a dir")
	}
	return nil
}

// MediaTranscodeRequest is a host request to transcode media (audio/video) that
// the SDK cannot encode itself, so it fits an inline limit.
type MediaTranscodeRequest struct {
	MIME     string
	Name     string
	Data     []byte
	MaxBytes int64
}

// MediaTranscodeResult is a transcoded payload. MIME empty means unchanged.
type MediaTranscodeResult struct {
	MIME string
	Data []byte
}

// CleanupSpoolDir removes files under a policy spool directory older than the
// spool retention window. A missing directory is not an error. It is the
// policy-spool analogue of CleanupA2ASpool.
func CleanupSpoolDir(dir string) error {
	if dir == "" {
		return nil
	}
	entries, err := ReadDir(dir)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-A2ASpoolRetention)
	for _, entry := range entries {
		if entry.Type == FileKindDirectory {
			continue
		}
		path := filepath.Join(dir, entry.Name)
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = RemoveAll(path)
		}
	}
	return nil
}
