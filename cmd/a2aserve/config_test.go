package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Listen != "127.0.0.1:9099" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0] != "build" {
		t.Fatalf("agents = %v", cfg.Agents)
	}
}

func TestLoadConfigOverDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a2a.yaml")
	content := "listen: 127.0.0.1:1234\nmodel: gpt-4o-mini\nagents: [build, plan]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listen != "127.0.0.1:1234" || cfg.Model != "gpt-4o-mini" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("agents = %v", cfg.Agents)
	}
	if cfg.KeepAlive != "15s" {
		t.Fatalf("keepAlive default lost = %q", cfg.KeepAlive)
	}
}

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a2a.yaml")
	if err := os.WriteFile(path, []byte("bogus: 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestFlagsOverrideFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a2a.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:1111\nmodel: file-model\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := defaultConfig()
	if err := loadConfig(path, &cfg); err != nil {
		t.Fatalf("load: %v", err)
	}

	flags := newFlags()
	if err := flags.parse([]string{
		"--listen", "127.0.0.1:2222",
		"--model", "flag-model",
		"--agent", "build", "--agent", "plan",
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	flags.apply(&cfg)

	if cfg.Listen != "127.0.0.1:2222" {
		t.Fatalf("listen = %q, want flag value", cfg.Listen)
	}
	if cfg.Model != "flag-model" {
		t.Fatalf("model = %q, want flag value", cfg.Model)
	}
	if len(cfg.Agents) != 2 || cfg.Agents[0] != "build" || cfg.Agents[1] != "plan" {
		t.Fatalf("agents = %v", cfg.Agents)
	}
}

func TestValidate(t *testing.T) {
	valid := config{Listen: "127.0.0.1:9099", Root: "/r", Model: "m", Protocol: "openai-responses", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	awsValid := config{Listen: "127.0.0.1:9099", Root: "/r", Model: "m", Protocol: "bedrock-converse", Agents: []string{"build"},
		Credentials: credentialsConfig{AWS: &awsCredentials{Region: "us-east-1", AccessKeyID: "AK", SecretAccessKey: "SK"}}}
	if err := awsValid.validate(); err != nil {
		t.Fatalf("valid aws config rejected: %v", err)
	}
	// A non-loopback bind is allowed with auth or an explicit opt-out.
	exposed := config{Listen: "0.0.0.0:9099", Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"},
		Credentials: credentialsConfig{APIKey: "k"}, Auth: &authConfig{Bearer: "t"}}
	if err := exposed.validate(); err != nil {
		t.Fatalf("non-loopback with auth rejected: %v", err)
	}
	exposed.AllowInsecure = true
	exposed.Auth = nil
	if err := exposed.validate(); err != nil {
		t.Fatalf("non-loopback with allowInsecure rejected: %v", err)
	}

	cases := map[string]config{
		"missing root":          {Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}},
		"missing model":         {Root: "/r", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}},
		"missing protocol":      {Root: "/r", Model: "m", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}},
		"no agents":             {Root: "/r", Model: "m", Protocol: "p", Credentials: credentialsConfig{APIKey: "k"}},
		"no credentials":        {Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}},
		"aws missing fields":    {Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{AWS: &awsCredentials{Region: "us-east-1"}}},
		"empty bearer":          {Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}, Auth: &authConfig{}},
		"negative keepAlive":    {Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}, KeepAlive: "-1s"},
		"non-loopback no auth":  {Listen: "0.0.0.0:9099", Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}},
		"all-interfaces host":   {Listen: ":9099", Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}},
		"negative contextLimit": {Listen: "127.0.0.1:9099", Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}, ContextLimit: -1},
		"tls missing key":       {Listen: "127.0.0.1:9099", Root: "/r", Model: "m", Protocol: "p", Agents: []string{"build"}, Credentials: credentialsConfig{APIKey: "k"}, TLS: &tlsConfig{CertFile: "c"}},
	}
	for name, cfg := range cases {
		if err := cfg.validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestKeepAliveDuration(t *testing.T) {
	cases := map[string]struct {
		value   string
		wantErr bool
	}{
		"empty":    {"", false},
		"valid":    {"15s", false},
		"invalid":  {"soon", true},
		"negative": {"-1s", true},
	}
	for name, tc := range cases {
		_, err := (config{KeepAlive: tc.value}).keepAliveDuration()
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", name, err, tc.wantErr)
		}
	}
}

func TestPermissionRuleset(t *testing.T) {
	cfg := config{InboundPermissions: []permissionRule{{Action: "bash", Resource: "*", Effect: "deny"}}}
	rules := cfg.permissionRuleset()
	if len(rules) != 1 || rules[0].Action != "bash" || rules[0].Effect != "deny" {
		t.Fatalf("rules = %+v", rules)
	}
	if (config{}).permissionRuleset() != nil {
		t.Fatal("empty config should yield nil (use SDK default)")
	}
}
