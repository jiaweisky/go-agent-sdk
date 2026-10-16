package main

import (
	"testing"

	config "github.com/jiaweisky/go-agent-sdk/cmd/agentd/config"
)

func TestIsolationExcludes(t *testing.T) {
	// A base directory inside the source is excluded (relative glob).
	if got := isolationExcludes("/proj", []string{"/proj/.views"}); len(got) != 1 || got[0] != ".views/**" {
		t.Fatalf("inside base = %v, want [.views/**]", got)
	}
	// A base directory outside the source is not excluded.
	if got := isolationExcludes("/proj", []string{"/var/lib/views"}); len(got) != 0 {
		t.Fatalf("outside base = %v, want none", got)
	}
	// Equal or empty paths are never excluded.
	if got := isolationExcludes("/proj", []string{"/proj", ""}); len(got) != 0 {
		t.Fatalf("equal/empty base = %v, want none", got)
	}
	// A nested base dir yields the nested relative glob.
	if got := isolationExcludes("/proj", []string{"/proj/a/b"}); len(got) != 1 || got[0] != "a/b/**" {
		t.Fatalf("nested base = %v, want [a/b/**]", got)
	}
}

// TestIngestSchemes: the advertised reference schemes follow the store backend.
func TestIngestSchemes(t *testing.T) {
	cases := []struct {
		st   config.StorageConfig
		want string
	}{
		{config.StorageConfig{Backend: "http", URL: "https://store/x"}, "https"},
		{config.StorageConfig{Backend: "http", URL: "http://store/x"}, "http"},
		{config.StorageConfig{Backend: "s3"}, "https"},
		{config.StorageConfig{Backend: "cos"}, "https"},
		{config.StorageConfig{Backend: "local", Root: "/srv"}, "file"},
		{config.StorageConfig{Backend: "local", Root: "/srv", ServeBaseURL: "http://host/s"}, "http"},
	}
	for _, c := range cases {
		got := ingestSchemes(&c.st)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("ingestSchemes(%+v) = %v, want [%s]", c.st, got, c.want)
		}
	}
}
