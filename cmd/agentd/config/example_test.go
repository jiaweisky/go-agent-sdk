package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestExampleProviderFileProfiles keeps the shipped canonical profiles in sync
// with the documented provider contracts.
func TestExampleProviderFileProfiles(t *testing.T) {
	var cfg Config
	if err := Load(filepath.Join("..", "agentd.example.yaml"), &cfg); err != nil {
		t.Fatalf("load example: %v", err)
	}
	cases := []struct {
		name    string
		url     string
		style   string
		seconds int
		max     int64
		timeout string
		mimes   []string
	}{
		{"deepseek", "https://api.deepseek.com/files", "expires_after", 86400, 67108864, "10m", []string{"image/jpeg", "image/png", "image/gif", "image/webp"}},
		{"deepseek-anthropic-files", "https://api.deepseek.com/anthropic/v1/files", "expires_in_seconds", 86400, 67108864, "10m", []string{"image/jpeg", "image/png", "image/gif", "image/webp"}},
		{"anthropic", "https://api.anthropic.com/v1/files", "expires_in_seconds", 86400, 524288000, "10m", []string{"application/pdf", "text/plain", "image/jpeg", "image/png", "image/gif", "image/webp"}},
		{"openai", "https://api.openai.com/v1/files", "none", 0, 52428800, "2m", []string{"application/pdf", "text/plain", "text/markdown", "text/csv", "application/json", "application/msword", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "image/jpeg", "image/png", "image/gif", "image/webp"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := cfg.ProviderFileAPIs[tc.name]
			if profile == nil {
				t.Fatalf("example is missing providerFileAPIs.%s", tc.name)
			}
			if profile.Kind != "multipart-json" || profile.URL != tc.url {
				t.Fatalf("profile = %+v", profile)
			}
			if profile.Expiry.Style != tc.style || profile.Expiry.Seconds != tc.seconds {
				t.Fatalf("expiry = %+v", profile.Expiry)
			}
			if profile.Limits.MaxBytes != tc.max || profile.Limits.Timeout != tc.timeout {
				t.Fatalf("limits = %+v", profile.Limits)
			}
			if !slices.Equal(profile.MIME.Allowed, tc.mimes) {
				t.Fatalf("mime.allowed = %v, want %v", profile.MIME.Allowed, tc.mimes)
			}
			if len(profile.Auth.Headers) == 0 {
				t.Fatal("profile auth headers are empty")
			}
		})
	}
	deepseek := cfg.ProviderFileAPIs["deepseek"]
	if deepseek.Fields["purpose"] != "user_data" || deepseek.IDField != "id" {
		t.Fatalf("deepseek fields = %+v idField=%q", deepseek.Fields, deepseek.IDField)
	}
	if cfg.ProviderFileAPIs["deepseek-anthropic-files"].ModelHeaders["anthropic-beta"] == "" {
		t.Fatal("deepseek-anthropic-files should carry the Anthropic beta model header")
	}
	for _, model := range cfg.Models {
		if model.ProviderFileAPI == "" {
			continue
		}
		if model.ProviderID == "" {
			t.Fatalf("model %q has providerFileAPI without providerID", model.ID)
		}
		if cfg.ProviderFileAPIs[model.ProviderFileAPI] == nil {
			t.Fatalf("model %q references unknown providerFileAPI %q", model.ID, model.ProviderFileAPI)
		}
	}
}

func TestMediaSummaryRedactsValues(t *testing.T) {
	var cfg Config
	if err := Load(filepath.Join("..", "agentd.example.yaml"), &cfg); err != nil {
		t.Fatalf("load example: %v", err)
	}
	lines := strings.Join(MediaSummary(cfg), "\n")
	for _, want := range []string{
		"openai (bearer",
		"deepseek (bearer",
		"deepseek-anthropic-files (x-api-key+beta",
		"anthropic (x-api-key+version",
	} {
		if !strings.Contains(lines, want) {
			t.Fatalf("summary missing %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "replace-me") || strings.Contains(lines, "sk-") {
		t.Fatalf("summary leaked a secret:\n%s", lines)
	}
}
