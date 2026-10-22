package config

import "testing"

func TestValidateControl(t *testing.T) {
	base := func(control *ControlConfig) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{{
				Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a",
				Agents: []string{"build"},
			}},
			Control: control,
		}
	}
	if err := base(nil).Validate(); err != nil {
		t.Fatalf("control disabled should validate: %v", err)
	}
	if err := base(&ControlConfig{}).Validate(); err == nil {
		t.Fatal("expected control.listen to be required")
	}
	if err := base(&ControlConfig{Listen: "nope"}).Validate(); err == nil {
		t.Fatal("expected a bad control.listen to be rejected")
	}
	if err := base(&ControlConfig{Listen: "127.0.0.1:9090", ProjectListen: "127.0.0.1:0"}).Validate(); err != nil {
		t.Fatalf("valid control config: %v", err)
	}
	if err := base(&ControlConfig{Listen: "0.0.0.0:9090"}).Validate(); err == nil {
		t.Fatal("expected a non-loopback control.listen without auth to be rejected")
	}
	if err := base(&ControlConfig{Listen: "0.0.0.0:9090", Auth: &AuthConfig{Type: "bearer", Token: "t"}}).Validate(); err != nil {
		t.Fatalf("non-loopback control with bearer should validate: %v", err)
	}
	if err := base(&ControlConfig{Listen: "0.0.0.0:9090", Auth: &AuthConfig{Type: "basic", Token: "t"}}).Validate(); err == nil {
		t.Fatal("expected an unsupported control auth type to be rejected")
	}
	if err := base(&ControlConfig{Listen: "0.0.0.0:9090", Auth: &AuthConfig{Type: "bearer"}}).Validate(); err == nil {
		t.Fatal("expected an empty control token to be rejected")
	}
	if err := base(&ControlConfig{Listen: "127.0.0.1:9090", ProjectListen: "0.0.0.0:0"}).Validate(); err == nil {
		t.Fatal("expected a non-loopback projectListen to be rejected")
	}
	if err := base(&ControlConfig{Listen: "127.0.0.1:9090", MaxLeases: -1}).Validate(); err == nil {
		t.Fatal("expected a negative control.maxLeases to be rejected")
	}
}
