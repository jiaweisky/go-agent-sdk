package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/core"
)

func TestPathGuardRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	policy := StandardPolicy(dir)
	guard := NewPathGuard(policy)
	ctx := context.Background()

	if err := guard.AllowRead(ctx, filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("the working directory must be readable: %v", err)
	}
	if err := guard.AllowRead(ctx, filepath.Join(home, "secret")); err == nil {
		t.Fatal("a path under the denied home region must be denied")
	}

	policy.DenyRead = []string{"**/.env"}
	guard = NewPathGuard(policy)
	if err := guard.AllowRead(ctx, filepath.Join(dir, ".env")); err == nil {
		t.Fatal("a more specific deny must win inside the allow-list")
	}
	if err := guard.AllowRead(ctx, filepath.Join(dir, "ok.txt")); err != nil {
		t.Fatalf("a non-denied file must stay readable: %v", err)
	}
}

func TestPathGuardWrite(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dir := t.TempDir()
	guard := NewPathGuard(StandardPolicy(dir))
	ctx := context.Background()

	if err := guard.Allow(ctx, filepath.Join(dir, "a.txt"), core.FileOpWrite); err != nil {
		t.Fatalf("the working directory must be writable: %v", err)
	}
	if err := guard.Allow(ctx, filepath.Join(home, ".agentd-guard-write-test"), core.FileOpWrite); err == nil {
		t.Fatal("a path outside the write allow-list must be denied")
	}
}

// TestPathGuardOff checks that an explicit off mode disables the dimension.
func TestPathGuardOff(t *testing.T) {
	outside := t.TempDir()
	guard := NewPathGuard(Policy{WorkingDirectory: t.TempDir(), ReadMode: FSModeOff, WriteMode: FSModeOff})
	if err := guard.AllowRead(context.Background(), filepath.Join(outside, "a")); err != nil {
		t.Fatalf("read: off must allow: %v", err)
	}
	if err := guard.Allow(context.Background(), filepath.Join(outside, "a"), core.FileOpWrite); err != nil {
		t.Fatalf("write: off must allow: %v", err)
	}
}

// TestSocketAllowlist checks the dedicated UNIX-socket policy: an explicitly
// allowed socket is reachable, while the default deny keeps it unreachable.
func TestSocketAllowlist(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	socket := filepath.Join(home, ".agentd-test.sock")
	dir := t.TempDir()
	policy := StandardPolicy(dir)
	policy.SocketMode = SocketAllowlist
	policy.SocketAllow = []string{socket}
	guard := NewPathGuard(policy)
	if err := guard.AllowRead(context.Background(), socket); err != nil {
		t.Fatalf("an allow-listed socket must be readable: %v", err)
	}
	if err := guard.Allow(context.Background(), socket, core.FileOpWrite); err != nil {
		t.Fatalf("an allow-listed socket must be writable: %v", err)
	}

	deny := StandardPolicy(dir)
	guard = NewPathGuard(deny)
	if err := guard.Allow(context.Background(), socket, core.FileOpWrite); err == nil {
		t.Fatal("a socket outside the allow-list must be denied")
	}
}

// TestDenyReadRegionsRootHome guards the parent expansion: a home directly under
// / (e.g. /root) must not turn into a deny-everything rule.
func TestDenyReadRegionsRootHome(t *testing.T) {
	skipOnWindows(t)
	regions := denyReadRegions("/root")
	if contains(regions, "/") {
		t.Fatalf("root home must not deny /: %v", regions)
	}
	if !contains(regions, "/root") {
		t.Fatalf("root home missing: %v", regions)
	}

	regions = denyReadRegions("/Users/alice")
	if !contains(regions, "/Users") {
		t.Fatalf("the home parent must be denied: %v", regions)
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/.env", "/a/b/.env", true},
		{"**/.env", "/a/b/.env.local", false},
		{"/work/**", "/work/a/b", true},
		{"/work/**", "/other/a", false},
		{"/work/*.txt", "/work/a.txt", true},
		{"/work/*.txt", "/work/a/b.txt", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestScrubEnv(t *testing.T) {
	t.Setenv("AGENTD_TEST_KEY", "secret")
	filtered, extend := scrubEnv(map[string]string{"KEEP": "1"}, true, []string{"*_KEY"})
	if extend {
		t.Fatal("a scrubbed environment must replace inheritance")
	}
	if _, ok := filtered["AGENTD_TEST_KEY"]; ok {
		t.Fatalf("the secret was not scrubbed: %v", filtered)
	}
	if filtered["KEEP"] != "1" {
		t.Fatalf("the explicit environment was lost: %v", filtered)
	}
}

func TestUnder(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		root string
		want bool
	}{
		{"equal", "/etc", "/etc", true},
		{"child", "/etc/hosts", "/etc", true},
		{"deep child", "/etc/pki/tls/certs", "/etc", true},
		{"prefix sibling", "/etcetera", "/etc", false},
		{"parent", "/etc", "/etc/pki", false},
		{"unrelated", "/data/poe", "/etc", false},
		{"empty path", "", "/etc", false},
		{"empty root", "/etc/hosts", "", false},
		{"trailing slash root", "/etc/hosts", "/etc/", true},
		{"trailing slash path", "/etc/hosts/", "/etc", true},
	} {
		if got := Under(tc.path, tc.root); got != tc.want {
			t.Errorf("%s: Under(%q, %q) = %v, want %v", tc.name, tc.path, tc.root, got, tc.want)
		}
	}
}

func TestUnderAny(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		roots []string
		want  bool
	}{
		{"one of many", "/etc/hosts", []string{"/data", "/etc", "/usr"}, true},
		{"none of many", "/etc/hosts", []string{"/data", "/usr"}, false},
		{"empty list", "/etc/hosts", nil, false},
		{"empty entry skipped", "/etc/hosts", []string{"", "/etc"}, true},
		{"trailing slash root", "/etc/hosts", []string{"/etc/"}, true},
		{"prefix sibling", "/etcetera", []string{"/etc"}, false},
	} {
		if got := UnderAny(tc.path, tc.roots); got != tc.want {
			t.Errorf("%s: UnderAny(%q, %v) = %v, want %v", tc.name, tc.path, tc.roots, got, tc.want)
		}
	}
}
