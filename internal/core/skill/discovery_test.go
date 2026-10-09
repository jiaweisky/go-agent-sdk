package skill

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestIsSafeSegment(t *testing.T) {
	cases := map[string]bool{
		"deploy": true,
		"a.b":    true,
		"":       false,
		".":      false,
		"..":     false,
		"a/b":    false,
		"a\\b":   false,
		"a\x00b": false,
	}
	for value, want := range cases {
		if got := isSafeSegment(value); got != want {
			t.Fatalf("isSafeSegment(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestIsSafeRelativePath(t *testing.T) {
	cases := map[string]bool{
		"foo/bar":     true,
		"sub/file.md": true,
		"http://x/y":  false,
		"/abs":        false,
		"//host/path": false,
		"a:b":         false,
		"C:\\x":       false,
		"\\\\srv\\x":  false,
		"x?y":         false,
		"x#y":         false,
		"":            false,
		".":           false,
		"..":          false,
		"a%2Fb":       false,
		"%2e%2e":      false,
		"foo/../bar":  false,
	}
	for value, want := range cases {
		if got := isSafeRelativePath(value); got != want {
			t.Fatalf("isSafeRelativePath(%q) = %v, want %v", value, got, want)
		}
	}
}

type testFS struct{}

func (testFS) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (testFS) ReadFileStringSafe(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (testFS) WriteWithDirs(path string, content []byte, mode ...os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func (testFS) WriteFileString(path string, content string, mode ...os.FileMode) error {
	return testFS{}.WriteWithDirs(path, []byte(content), mode...)
}

func (testFS) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (testFS) RemoveAll(path string) error          { return os.RemoveAll(path) }

func (testFS) Contains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func TestDiscoveryPull(t *testing.T) {
	cache := t.TempDir()
	mux := http.NewServeMux()
	mux.HandleFunc("/skills/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"skills":[{"name":"deploy","files":["SKILL.md"]},{"name":"../evil","files":["SKILL.md"]}]}`))
	})
	mux.HandleFunc("/skills/deploy/SKILL.md", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("---\nname: deploy\n---\n# deploy"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	discovery := NewDiscovery(testFS{}, cache, server.Client())
	directories, err := discovery.Pull(server.URL + "/skills/")
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(directories) != 1 {
		t.Fatalf("directories = %+v", directories)
	}
	content, err := os.ReadFile(filepath.Join(string(directories[0]), "SKILL.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(content), "name: deploy") {
		t.Fatalf("content = %q", content)
	}
}

func TestDiscoveryPullIndexFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	discovery := NewDiscovery(testFS{}, t.TempDir(), server.Client())
	directories, err := discovery.Pull(server.URL + "/skills/")
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(directories) != 0 {
		t.Fatalf("directories = %+v", directories)
	}
}

type discoveryServer struct {
	mu       sync.Mutex
	skills   []IndexSkill
	files    map[string]string
	requests []string
	server   *httptest.Server
}

func newDiscoveryServer(t *testing.T, skills []IndexSkill, files map[string]string) *discoveryServer {
	t.Helper()
	ds := &discoveryServer{skills: skills, files: files}
	mux := http.NewServeMux()
	mux.HandleFunc("/catalog/index.json", func(w http.ResponseWriter, r *http.Request) {
		ds.record(r.URL.Path)
		ds.mu.Lock()
		skills := ds.skills
		ds.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Index{Skills: skills})
	})
	mux.HandleFunc("/catalog/", func(w http.ResponseWriter, r *http.Request) {
		ds.record(r.URL.Path)
		ds.mu.Lock()
		body, ok := ds.files[r.URL.Path]
		ds.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	ds.server = httptest.NewServer(mux)
	t.Cleanup(ds.server.Close)
	return ds
}

func (d *discoveryServer) record(path string) {
	d.mu.Lock()
	d.requests = append(d.requests, path)
	d.mu.Unlock()
}

func (d *discoveryServer) set(skills []IndexSkill, files map[string]string) {
	d.mu.Lock()
	d.skills = skills
	d.files = files
	d.requests = nil
	d.mu.Unlock()
}

func (d *discoveryServer) base() string { return d.server.URL + "/catalog/" }

func (d *discoveryServer) recorded() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.requests...)
}

func TestDiscoveryRejectsUnsafeEntries(t *testing.T) {
	cases := []struct {
		name   string
		skills []IndexSkill
	}{
		{"name traversal", []IndexSkill{{Name: "../outside", Files: []string{"SKILL.md"}}}},
		{"file traversal", []IndexSkill{{Name: "deploy", Files: []string{"SKILL.md", "../outside.md"}}}},
		{"absolute file", []IndexSkill{{Name: "deploy", Files: []string{"SKILL.md", "/tmp/outside.md"}}}},
		{"cross origin", []IndexSkill{{Name: "deploy", Files: []string{"SKILL.md", "https://evil.example.test/outside.md"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newDiscoveryServer(t, tc.skills, map[string]string{})
			cache := t.TempDir()
			discovery := NewDiscovery(testFS{}, cache, server.server.Client())
			directories, err := discovery.Pull(server.base())
			if err != nil {
				t.Fatalf("pull: %v", err)
			}
			if len(directories) != 0 {
				t.Fatalf("directories = %+v", directories)
			}
			if got := server.recorded(); len(got) != 1 || got[0] != "/catalog/index.json" {
				t.Fatalf("requests = %v", got)
			}
			if entries, _ := os.ReadDir(cache); len(entries) != 0 {
				t.Fatalf("cache not empty: %v", entries)
			}
		})
	}
}

func TestDiscoveryDownloadsNestedFiles(t *testing.T) {
	server := newDiscoveryServer(t,
		[]IndexSkill{{Name: "deploy", Files: []string{"SKILL.md", "references/guide.md"}}},
		map[string]string{
			"/catalog/deploy/SKILL.md":            "# Deploy",
			"/catalog/deploy/references/guide.md": "# Guide",
		},
	)
	discovery := NewDiscovery(testFS{}, t.TempDir(), server.server.Client())
	directories, err := discovery.Pull(server.base())
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(directories) != 1 {
		t.Fatalf("directories = %+v", directories)
	}
	if content, _ := os.ReadFile(filepath.Join(string(directories[0]), "SKILL.md")); string(content) != "# Deploy" {
		t.Fatalf("SKILL.md = %q", content)
	}
	if content, _ := os.ReadFile(filepath.Join(string(directories[0]), "references", "guide.md")); string(content) != "# Guide" {
		t.Fatalf("guide.md = %q", content)
	}
	got := server.recorded()
	sort.Strings(got)
	want := []string{"/catalog/deploy/SKILL.md", "/catalog/deploy/references/guide.md", "/catalog/index.json"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requests = %v, want %v", got, want)
	}
}

func TestDiscoveryVersionRefresh(t *testing.T) {
	server := newDiscoveryServer(t, nil, nil)
	cache := t.TempDir()
	discovery := NewDiscovery(testFS{}, cache, server.server.Client())

	server.set([]IndexSkill{{Name: "deploy", Version: strPtr("1"), Files: []string{"SKILL.md"}}},
		map[string]string{"/catalog/deploy/SKILL.md": "# Old"})
	first, err := discovery.Pull(server.base())
	if err != nil || len(first) != 1 {
		t.Fatalf("first pull: %v %+v", err, first)
	}

	server.set([]IndexSkill{{Name: "deploy", Version: strPtr("2"), Files: []string{"SKILL.md"}}},
		map[string]string{"/catalog/deploy/SKILL.md": "# New"})
	if _, err := discovery.Pull(server.base()); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if content, _ := os.ReadFile(filepath.Join(string(first[0]), "SKILL.md")); string(content) != "# New" {
		t.Fatalf("SKILL.md = %q", content)
	}
	if got := server.recorded(); !slices.Contains(got, "/catalog/deploy/SKILL.md") {
		t.Fatalf("expected refresh request, got %v", got)
	}

	server.set([]IndexSkill{{Name: "deploy", Version: strPtr("2"), Files: []string{"SKILL.md"}}},
		map[string]string{"/catalog/deploy/SKILL.md": "# Ignored"})
	if _, err := discovery.Pull(server.base()); err != nil {
		t.Fatalf("third pull: %v", err)
	}
	if got := server.recorded(); len(got) != 1 || got[0] != "/catalog/index.json" {
		t.Fatalf("expected only index request, got %v", got)
	}
}

func TestDiscoveryPartialUpdateKeepsOldFiles(t *testing.T) {
	server := newDiscoveryServer(t, nil, nil)
	cache := t.TempDir()
	discovery := NewDiscovery(testFS{}, cache, server.server.Client())

	server.set([]IndexSkill{{Name: "deploy", Version: strPtr("1"), Files: []string{"SKILL.md", "old.md"}}},
		map[string]string{
			"/catalog/deploy/SKILL.md": "# Old",
			"/catalog/deploy/old.md":   "old reference",
		})
	first, err := discovery.Pull(server.base())
	if err != nil || len(first) != 1 {
		t.Fatalf("first pull: %v %+v", err, first)
	}
	root := string(first[0])

	server.set([]IndexSkill{{Name: "deploy", Version: strPtr("2"), Files: []string{"SKILL.md", "missing.md"}}},
		map[string]string{"/catalog/deploy/SKILL.md": "# Partial"})
	if _, err := discovery.Pull(server.base()); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if content, _ := os.ReadFile(filepath.Join(root, "SKILL.md")); string(content) != "# Old" {
		t.Fatalf("SKILL.md after partial = %q", content)
	}
	if content, _ := os.ReadFile(filepath.Join(root, "old.md")); string(content) != "old reference" {
		t.Fatalf("old.md after partial = %q", content)
	}

	server.set([]IndexSkill{{Name: "deploy", Version: strPtr("3"), Files: []string{"SKILL.md"}}},
		map[string]string{"/catalog/deploy/SKILL.md": "# New"})
	if _, err := discovery.Pull(server.base()); err != nil {
		t.Fatalf("third pull: %v", err)
	}
	if content, _ := os.ReadFile(filepath.Join(root, "SKILL.md")); string(content) != "# New" {
		t.Fatalf("SKILL.md after update = %q", content)
	}
	if _, err := os.Stat(filepath.Join(root, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("expected old.md removed, err = %v", err)
	}
}

func strPtr(value string) *string { return &value }
