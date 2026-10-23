package config

import "testing"

func TestNormalizePathPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", "/"},
		{"/foo/", "/foo"},
		{"foo", "/foo"},
		{"//foo//", "/foo"},
		{"/foo//bar", "/foo//bar"},
	}
	for _, tc := range cases {
		if got := NormalizePathPrefix(tc.in); got != tc.want {
			t.Errorf("NormalizePathPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{" ", ""},
		{"Example.COM", "example.com"},
		{"Example.COM:8443", "example.com"},
		{"[::1]:8080", "::1"},
		{"[::1]", "::1"},
	}
	for _, tc := range cases {
		if got := NormalizeHost(tc.in); got != tc.want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
