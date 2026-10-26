package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func validProviderFileAPI() *ProviderFileAPI {
	return &ProviderFileAPI{
		Kind:   "multipart-json",
		URL:    "https://up.example/files",
		Auth:   ProviderFileAuth{Headers: map[string]string{"Authorization": "Bearer x"}},
		Expiry: ProviderFileExpiry{Style: "none"},
		MIME:   ProviderFileMIME{Allowed: []string{"application/pdf"}},
		Limits: ProviderFileLimits{MaxBytes: 1024, Timeout: "2m"},
	}
}

func TestValidateProviderFileAPI(t *testing.T) {
	valid := validProviderFileAPI()
	if err := valid.Validate("test"); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*ProviderFileAPI)
	}{
		{"bad kind", func(p *ProviderFileAPI) { p.Kind = "json" }},
		{"bad url", func(p *ProviderFileAPI) { p.URL = "ftp://up.example" }},
		{"relative url", func(p *ProviderFileAPI) { p.URL = "not a url" }},
		{"no headers", func(p *ProviderFileAPI) { p.Auth.Headers = nil }},
		{"empty header value", func(p *ProviderFileAPI) { p.Auth.Headers["X-Key"] = "" }},
		{"missing expiry style", func(p *ProviderFileAPI) { p.Expiry = ProviderFileExpiry{} }},
		{"negative expiry", func(p *ProviderFileAPI) { p.Expiry = ProviderFileExpiry{Style: "expires_after", Seconds: -1} }},
		{"expiry without seconds", func(p *ProviderFileAPI) { p.Expiry = ProviderFileExpiry{Style: "expires_after"} }},
		{"empty mime", func(p *ProviderFileAPI) { p.MIME.Allowed = nil }},
		{"upper mime", func(p *ProviderFileAPI) { p.MIME.Allowed = []string{"Application/PDF"} }},
		{"no max bytes", func(p *ProviderFileAPI) { p.Limits.MaxBytes = 0 }},
		{"bad timeout", func(p *ProviderFileAPI) { p.Limits.Timeout = "nope" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProviderFileAPI()
			tc.edit(p)
			if err := p.Validate("test"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestValidateMediaDelivery(t *testing.T) {
	if err := (*MediaDeliveryConfig)(nil).Validate(); err != nil {
		t.Fatalf("nil media should be valid: %v", err)
	}
	if err := (&MediaDeliveryConfig{Prefer: []string{"url", "providerFile", "inline"}}).Validate(); err != nil {
		t.Fatalf("url is a valid prefer source: %v", err)
	}
	if err := (&MediaDeliveryConfig{Prefer: []string{"bogus"}}).Validate(); err == nil {
		t.Fatal("an unknown prefer source must be rejected")
	}
	if err := (&MediaDeliveryConfig{Prefer: []string{"url", "url"}}).Validate(); err == nil {
		t.Fatal("a duplicate prefer source must be rejected")
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
	if err := (&MediaDeliveryConfig{Prefer: []string{"providerFile"}, Spool: &MediaSpoolConfig{Dir: "{Root}/data/spool"}}).Validate(); err != nil {
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
	if err := yaml.Unmarshal([]byte("maxBytes: {imag: 1}"), &cfg); err == nil {
		t.Fatal("an unknown size-limit key must be rejected")
	}
}

func TestProviderFileReferences(t *testing.T) {
	profiles := map[string]*ProviderFileAPI{
		"a": validProviderFileAPI(),
		"b": validProviderFileAPI(),
	}
	models := []ModelConfig{{
		ID: "m", Model: "m", Protocol: "openai-compatible-chat",
		ProviderID: "p", ProviderFileAPI: "a",
	}}
	if err := validateModelProviderFileAPIs(models, profiles); err != nil {
		t.Fatalf("valid reference rejected: %v", err)
	}
	missing := append([]ModelConfig(nil), models...)
	missing[0].ProviderFileAPI = "missing"
	if err := validateModelProviderFileAPIs(missing, profiles); err == nil {
		t.Fatal("an unknown providerFileAPI must be rejected")
	}
	noProvider := append([]ModelConfig(nil), models...)
	noProvider[0].ProviderID = ""
	if err := validateModelProviderFileAPIs(noProvider, profiles); err == nil {
		t.Fatal("providerFileAPI without providerID must be rejected")
	}
}

func TestServerProviderFileMapping(t *testing.T) {
	a := validProviderFileAPI()
	b := validProviderFileAPI()
	profiles := map[string]*ProviderFileAPI{"a": a, "b": b}
	fileModel := ModelConfig{
		ID: "m", Model: "m", Protocol: "openai-compatible-chat",
		ProviderID: "p", ProviderFileAPI: "a",
		Media: &MediaConfig{Forms: map[string][]string{"file": {"file"}}},
	}
	catalog, err := NewCatalog(fileModel, nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	server := Server{Name: "s", Models: []string{"m"}}
	if err := validateServerProviderFiles(server, catalog, profiles); err != nil {
		t.Fatalf("valid server rejected: %v", err)
	}

	missing := fileModel
	missing.ProviderFileAPI = ""
	catalog, err = NewCatalog(missing, nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if err := validateServerProviderFiles(server, catalog, profiles); err == nil {
		t.Fatal("a file-capable model without providerFileAPI must be rejected")
	}

	second := fileModel
	second.ID = "m2"
	second.ProviderFileAPI = "b"
	catalog, err = NewCatalog(fileModel, []ModelConfig{second})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	server.Models = []string{"m", "m2"}
	if err := validateServerProviderFiles(server, catalog, profiles); err == nil {
		t.Fatal("one providerID mapped to two profiles must be rejected")
	}
}

func TestMediaWarnings(t *testing.T) {
	cfg := Config{
		Models: []ModelConfig{{
			ID: "m", Model: "m", Protocol: "openai-compatible-chat",
			ProviderID: "p", ProviderFileAPI: "p-files",
		}},
		ProviderFileAPIs: map[string]*ProviderFileAPI{"p-files": validProviderFileAPI()},
		Servers: []Server{{
			Name: "s", Models: []string{"m"},
			Media: &ServerMediaConfig{Delivery: &MediaDeliveryConfig{
				Prefer: []string{"providerFile", "url", "inline"},
			}},
		}},
	}
	warnings := strings.Join(MediaWarnings(cfg), "\n")
	for _, want := range []string{
		"media.uploads.urlHost is not provider-fetchable",
		"protocol \"openai-compatible-chat\" does not carry a file source",
	} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("warnings missing %q:\n%s", want, warnings)
		}
	}

	cfg.Servers[0].Models = nil
	cfg.Models[0].ProviderFileAPI = ""
	warnings = strings.Join(MediaWarnings(cfg), "\n")
	if !strings.Contains(warnings, "no selected model references a providerFileAPI") {
		t.Fatalf("missing no-profile warning:\n%s", warnings)
	}

	cfg.Servers[0].Media = nil
	if warnings := MediaWarnings(cfg); len(warnings) != 0 {
		t.Fatalf("unset media should not warn: %v", warnings)
	}
}
