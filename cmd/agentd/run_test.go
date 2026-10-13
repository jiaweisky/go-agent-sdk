package main

import "testing"

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
