package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type ripgrepTestBinary struct{ path string }

func (b ripgrepTestBinary) Filepath() (string, error) { return b.path, nil }

func newTestRipgrep(t *testing.T) Ripgrep {
	t.Helper()
	path, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep (rg) not available")
	}
	return NewRipgrep(NewAppProcess(), ripgrepTestBinary{path: path})
}

func writeRipgrepFixture(t *testing.T, root, name, content string) {
	t.Helper()
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRipgrepGlob(t *testing.T) {
	root := t.TempDir()
	writeRipgrepFixture(t, root, "a.go", "package a\n")
	writeRipgrepFixture(t, root, "sub/b.go", "package b\n")
	writeRipgrepFixture(t, root, "c.txt", "c\n")

	entries, err := newTestRipgrep(t).Glob(RipgrepGlobInput{Cwd: root, Pattern: "**/*.go", Limit: 100})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	got := map[string]FileKind{}
	for _, entry := range entries {
		got[entry.Path] = entry.Type
	}
	if len(got) != 2 || got["a.go"] != FileKindFile || got["sub/b.go"] != FileKindFile {
		t.Fatalf("unexpected entries: %v", got)
	}
}

func TestRipgrepGlobLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeRipgrepFixture(t, root, name, "package x\n")
	}

	entries, err := newTestRipgrep(t).Glob(RipgrepGlobInput{Cwd: root, Pattern: "*.go", Limit: 2})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected limit to bound results to 2, got %d", len(entries))
	}
}

func TestRipgrepGlobNegativeLimit(t *testing.T) {
	root := t.TempDir()
	writeRipgrepFixture(t, root, "a.go", "package a\n")

	entries, err := newTestRipgrep(t).Glob(RipgrepGlobInput{Cwd: root, Pattern: "*.go", Limit: -1})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries for negative limit, got %d", len(entries))
	}
}

func TestRipgrepFindOnEntry(t *testing.T) {
	root := t.TempDir()
	writeRipgrepFixture(t, root, "a.go", "package a\n")
	writeRipgrepFixture(t, root, "b.txt", "b\n")

	var seen []string
	entries, err := newTestRipgrep(t).Find(RipgrepFindInput{
		Cwd:     root,
		Pattern: "*.go",
		Limit:   100,
		OnEntry: func(entry FileSystemEntry) error {
			seen = append(seen, entry.Path)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "a.go" {
		t.Fatalf("unexpected entries: %v", entries)
	}
	if len(seen) != 1 || seen[0] != "a.go" {
		t.Fatalf("unexpected onEntry calls: %v", seen)
	}
}

func TestRipgrepGrep(t *testing.T) {
	root := t.TempDir()
	writeRipgrepFixture(t, root, "a.txt", "hello world\nsecond line\n")

	matches, err := newTestRipgrep(t).Grep(RipgrepGrepInput{Cwd: root, Pattern: "world", Limit: 100})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	match := matches[0]
	if match.Entry.Path != "a.txt" || match.Entry.Type != "file" {
		t.Fatalf("unexpected entry: %#v", match.Entry)
	}
	if match.Line != 1 || match.Offset != 0 {
		t.Fatalf("unexpected line/offset: %d/%d", match.Line, match.Offset)
	}
	if match.Text != "hello world\n" {
		t.Fatalf("unexpected text: %q", match.Text)
	}
	if len(match.Submatches) != 1 || match.Submatches[0].Text != "world" {
		t.Fatalf("unexpected submatches: %#v", match.Submatches)
	}
}

func TestRipgrepGrepInvalidPattern(t *testing.T) {
	root := t.TempDir()
	writeRipgrepFixture(t, root, "a.txt", "hello\n")

	_, err := newTestRipgrep(t).Grep(RipgrepGrepInput{Cwd: root, Pattern: "[", Limit: 100})
	var invalid *RipgrepInvalidPatternError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected invalid pattern error, got %v", err)
	}
	if invalid.Pattern != "[" {
		t.Fatalf("unexpected pattern: %s", invalid.Pattern)
	}
}

func TestRipgrepSplitLines(t *testing.T) {
	got := ripgrepSplitLines("a\nb\r\nc\rd")
	expected := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("ripgrepSplitLines = %v, want %v", got, expected)
	}
}

func TestRipgrepNormalizeRelative(t *testing.T) {
	cases := map[string]string{
		"./a.go":          "a.go",
		"././a.go":        "a.go",
		"sub/b.go":        "sub/b.go",
		"./sub\\b.go":     "sub/b.go",
		"/abs/path.go":    "abs/path.go",
		"..\\weird\\x.go": "../weird/x.go",
	}
	for input, expected := range cases {
		if got := ripgrepNormalizeRelative(input); got != expected {
			t.Fatalf("ripgrepNormalizeRelative(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestRipgrepTruncateText(t *testing.T) {
	if got := ripgrepTruncateText("short"); got != "short" {
		t.Fatalf("unexpected short text: %q", got)
	}

	long := strings.Repeat("x", 2500)
	got := ripgrepTruncateText(long)
	if got != strings.Repeat("x", 2000)+"..." {
		t.Fatalf("unexpected truncated length: %d", len(got))
	}

	emoji := strings.Repeat("a", 1999) + "\U0001F600"
	got = ripgrepTruncateText(emoji)
	if got != strings.Repeat("a", 1999)+"..." {
		t.Fatalf("expected trailing high surrogate dropped, got %q", got)
	}
}
