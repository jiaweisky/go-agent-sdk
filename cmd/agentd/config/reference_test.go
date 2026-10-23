package config

import "testing"

func TestValidateReferences(t *testing.T) {
	valid := []Reference{
		{Name: "docs", Type: "local", Path: "/src/docs"},
		{Name: "repo", Type: "git", Repository: "https://example/repo"},
	}
	if err := ValidateReferences(valid); err != nil {
		t.Fatalf("valid references rejected: %v", err)
	}
	cases := []struct {
		name string
		ref  Reference
	}{
		{"missing name", Reference{Type: "local", Path: "/src/docs"}},
		{"local without path", Reference{Name: "docs", Type: "local"}},
		{"git without repository", Reference{Name: "repo", Type: "git"}},
		{"unknown type", Reference{Name: "bad", Type: "ftp"}},
	}
	for _, tc := range cases {
		if err := ValidateReferences([]Reference{tc.ref}); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}
