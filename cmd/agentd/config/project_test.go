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
		Peers: []Peer{{Name: "echo", Endpoint: "http://127.0.0.1:9099"}},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a valid peer must validate: %v", err)
	}
	server := base.ToServer("/state")
	if len(server.Peers) != 1 || server.Peers[0].Name != "echo" {
		t.Fatalf("ToServer peers = %+v", server.Peers)
	}

	bad := base
	bad.Peers = []Peer{{Name: "echo"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected a peer without endpoint/cardURL to be rejected")
	}
}

func TestProjectRequestValidateSandbox(t *testing.T) {
	base := ProjectRequest{Name: "grp-a", Kind: "http", Root: "/state", Agents: []string{"build"}}
	if err := base.Validate(); err != nil {
		t.Fatalf("no sandbox must validate: %v", err)
	}
	base.Sandbox = &SandboxConfig{Process: "none"}
	if err := base.Validate(); err != nil {
		t.Fatalf("sandbox.process=none must validate: %v", err)
	}
	base.Sandbox = &SandboxConfig{}
	if err := base.Validate(); err == nil {
		t.Fatal("expected an empty sandbox.process to be rejected")
	}
	base.Sandbox = &SandboxConfig{Process: "bwrap", FailurePolicy: "maybe"}
	if err := base.Validate(); err == nil {
		t.Fatal("expected an unknown sandbox.failurePolicy to be rejected")
	}
}
