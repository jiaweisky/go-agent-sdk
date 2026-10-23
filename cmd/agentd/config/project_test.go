package config

import "testing"

func TestProjectRequestValidateReadonly(t *testing.T) {
	base := ProjectRequest{
		Name: "grp-a", Kind: "http", Root: "/state", Agents: []string{"build"},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a plain request must validate: %v", err)
	}
	base.Readonly = true
	if err := base.Validate(); err != nil {
		t.Fatalf("readonly without isolation must validate: %v", err)
	}
	base.Isolation = &IsolationConfig{Mode: "copy", BaseDir: "/views"}
	if err := base.Validate(); err == nil {
		t.Fatal("readonly with isolation must be rejected")
	}
}

func TestProjectRequestPeers(t *testing.T) {
	base := ProjectRequest{
		Name: "grp-a", Kind: "http", Root: "/state", Agents: []string{"build"},
		Peers: []A2APeer{{Name: "echo", Endpoint: "http://127.0.0.1:9099"}},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a valid A2A peer must validate: %v", err)
	}
	server := base.ToServer("/state")
	if len(server.Peers) != 1 || server.Peers[0].Name != "echo" {
		t.Fatalf("ToServer peers = %+v", server.Peers)
	}

	bad := base
	bad.Peers = []A2APeer{{Name: "echo"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected an A2A peer without endpoint/cardURL to be rejected")
	}
}

func TestProjectRequestValidateSandbox(t *testing.T) {
	base := ProjectRequest{Name: "grp-a", Kind: "http", Root: "/state", Agents: []string{"build"}}
	if err := base.Validate(); err != nil {
		t.Fatalf("no sandbox must validate: %v", err)
	}
	base.Sandbox = &SandboxConfig{Wrapper: "none"}
	if err := base.Validate(); err != nil {
		t.Fatalf("sandbox.wrapper=none must validate: %v", err)
	}
	base.Sandbox = &SandboxConfig{}
	if err := base.Validate(); err == nil {
		t.Fatal("expected an empty sandbox.wrapper to be rejected")
	}
	base.Sandbox = &SandboxConfig{Wrapper: "bwrap", FailurePolicy: "maybe"}
	if err := base.Validate(); err == nil {
		t.Fatal("expected an unknown sandbox.failurePolicy to be rejected")
	}
}
