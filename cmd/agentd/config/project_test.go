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
