package config

import "testing"

func isolationConfig(mode, baseDir string, readOnly bool) Config {
	return Config{
		Models: []ModelConfig{testModel()},
		Servers: []Server{{
			Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a",
			Agents:           []string{"build"},
			WorkingDirectory: "/src",
			Isolation:        &IsolationConfig{Mode: mode, BaseDir: baseDir, BaseReadOnly: readOnly},
		}},
	}
}

func TestValidateIsolation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"none", isolationConfig("none", "", false), false},
		{"copy ok", isolationConfig("copy", "/views", false), false},
		{"copy needs baseDir", isolationConfig("copy", "", false), true},
		{"worktree ok", isolationConfig("worktree", "/views", false), false},
		{"overlay needs readOnly", isolationConfig("overlay", "/views", false), true},
		{"overlay ok", isolationConfig("overlay", "/views", true), false},
		{"container is external", isolationConfig("container", "/views", false), true},
		{"unknown mode", isolationConfig("huh", "/views", false), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected a validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateIsolationRequiresWorkingDirectory(t *testing.T) {
	cfg := isolationConfig("copy", "/views", false)
	cfg.Servers[0].WorkingDirectory = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected isolation without a working directory to be rejected")
	}
}

func TestValidateIsolationCowMode(t *testing.T) {
	cfg := isolationConfig("overlay", "/views", true)
	cfg.Servers[0].Isolation.Cow = &CowConfig{Mode: "clonefile"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("clonefile should be accepted: %v", err)
	}
	cfg.Servers[0].Isolation.Cow = &CowConfig{Mode: "bogus"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an unknown cow mode to be rejected")
	}
}

func TestValidateSandbox(t *testing.T) {
	base := func(sandbox *SandboxConfig) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{{
				Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a",
				Agents: []string{"build"}, Sandbox: sandbox,
			}},
		}
	}
	if err := base(&SandboxConfig{Process: "none"}).Validate(); err != nil {
		t.Fatalf("sandbox.process=none should be accepted: %v", err)
	}
	if err := base(&SandboxConfig{}).Validate(); err == nil {
		t.Fatal("expected an empty sandbox.process to be rejected")
	}
	if err := base(&SandboxConfig{Process: "bwrap", FailurePolicy: "fail-open"}).Validate(); err != nil {
		t.Fatalf("fail-open should be accepted: %v", err)
	}
	if err := base(&SandboxConfig{Process: "bwrap", FailurePolicy: "maybe"}).Validate(); err == nil {
		t.Fatal("expected an unknown sandbox.failurePolicy to be rejected")
	}
}
