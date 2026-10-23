package config

import "testing"

func TestValidateWrap(t *testing.T) {
	base := func(wrap *WrapConfig) Config {
		return Config{
			Models: []ModelConfig{testModel()},
			Servers: []Server{{
				Name: "a", Kind: "http", Listen: "127.0.0.1:1", Root: "/a",
				Agents: []string{"build"},
			}},
			Wrap: wrap,
		}
	}
	if err := base(nil).Validate(); err != nil {
		t.Fatalf("wrap disabled should validate: %v", err)
	}
	if err := base(&WrapConfig{}).Validate(); err == nil {
		t.Fatal("expected wrap.wrapper to be required")
	}
	if err := base(&WrapConfig{Wrapper: "bwrap"}).Validate(); err != nil {
		t.Fatalf("valid wrap config: %v", err)
	}
	if err := base(&WrapConfig{Wrapper: "bwrap", FailurePolicy: "fail-open"}).Validate(); err != nil {
		t.Fatalf("fail-open should validate: %v", err)
	}
	if err := base(&WrapConfig{Wrapper: "bwrap", FailurePolicy: "nope"}).Validate(); err == nil {
		t.Fatal("expected a bad wrap.failurePolicy to be rejected")
	}
}
