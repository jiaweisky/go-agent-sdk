package core

import "testing"

func TestMimeType(t *testing.T) {
	cases := map[string]string{
		"a.txt":     "text/plain",
		"a.md":      "text/markdown",
		"a.csv":     "text/csv",
		"a.js":      "text/javascript",
		"a.ts":      "video/mp2t",
		"a.go":      "application/octet-stream",
		"a.py":      "application/octet-stream",
		"a.yaml":    "text/yaml",
		"a.json":    "application/json",
		"a.png":     "image/png",
		"a.jpg":     "image/jpeg",
		"a.unknown": "application/octet-stream",
		"README":    "application/octet-stream",
	}
	for path, want := range cases {
		if got := MimeType(path); got != want {
			t.Errorf("MimeType(%q) = %q, want %q", path, got, want)
		}
	}
}
