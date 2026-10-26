package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// MediaSummary describes each configured provider Files API profile without
// exposing credentials. Startup and --check print one line per profile.
func MediaSummary(cfg Config) []string {
	names := make([]string, 0, len(cfg.ProviderFileAPIs))
	for name := range cfg.ProviderFileAPIs {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		profile := cfg.ProviderFileAPIs[name]
		if profile == nil {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"agentd: media upload: %s (%s, expiry=%s, mime=%s, maxBytes=%s)",
			name, authSummary(profile.Auth.Headers), expirySummary(profile.Expiry),
			mimeSummary(profile.MIME.Allowed), bytesSummary(profile.Limits.MaxBytes),
		))
	}
	return lines
}

func authSummary(headers map[string]string) string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	has := func(name string) bool { return slices.Contains(names, name) }
	switch {
	case has("authorization"):
		return "bearer"
	case has("x-api-key") && has("anthropic-version") && has("anthropic-beta"):
		return "x-api-key+version+beta"
	case has("x-api-key") && has("anthropic-version"):
		return "x-api-key+version"
	case has("x-api-key") && has("anthropic-beta"):
		return "x-api-key+beta"
	case has("x-api-key"):
		return "x-api-key"
	case len(names) == 0:
		return "no-auth"
	default:
		return strings.Join(names, "+")
	}
}

func expirySummary(expiry ProviderFileExpiry) string {
	if expiry.Style == "none" || expiry.Style == "" {
		return "none"
	}
	return fmt.Sprintf("%s/%ds", expiry.Style, expiry.Seconds)
}

func mimeSummary(mimes []string) string {
	if len(mimes) <= 4 {
		return strings.Join(mimes, ",")
	}
	return fmt.Sprintf("%d types", len(mimes))
}

func bytesSummary(n int64) string {
	if n <= 0 {
		return "unlimited"
	}
	const mb = 1 << 20
	if n%mb == 0 {
		return fmt.Sprintf("%dMB", n/mb)
	}
	return fmt.Sprintf("%dB", n)
}
