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
	for _, ref := range valid {
		if err := ref.Validate(); err != nil {
			t.Fatalf("valid reference %q rejected: %v", ref.Name, err)
		}
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
		if err := tc.ref.Validate(); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	if err := ValidateReferences([]Reference{{Name: "bad", Type: "ftp"}}); err == nil {
		t.Fatal("expected ValidateReferences to surface the entry error")
	}
}
