package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaweisky/go-agent-sdk/pkg/schema"
)

func openTestDB(t *testing.T, maxConns ...int) *Database {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), maxConns...)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenCreatesSchema(t *testing.T) {
	db := openTestDB(t)

	for _, table := range []string{
		"project", "project_directory", "event_sequence", "event",
		"session", "todo", "session_message", "session_context_epoch",
		"session_input", "workspace", "message", "part", "permission", "turn",
	} {
		var name string
		err := db.DB.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()
}

func TestOpenMaxConns(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		conns []int
		want  int
	}{
		{name: "default", want: DefaultMaxConns},
		{name: "explicit", conns: []int{2}, want: 2},
		{name: "non-positive keeps default", conns: []int{0}, want: DefaultMaxConns},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openTestDB(t, testCase.conns...)
			if got := db.DB.Stats().MaxOpenConnections; got != testCase.want {
				t.Fatalf("MaxOpenConnections = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestDSNCarriesPragmas(t *testing.T) {
	built := dsn("foo.db")
	if !strings.HasPrefix(built, "foo.db?") {
		t.Fatalf("dsn = %q, want a query string", built)
	}
	for _, want := range []string{
		"_txlock=immediate",
		"_pragma=journal_mode%28WAL%29",
		"_pragma=synchronous%28NORMAL%29",
		"_pragma=busy_timeout%285000%29",
		"_pragma=foreign_keys%281%29",
	} {
		if !strings.Contains(built, want) {
			t.Fatalf("dsn %q missing %q", built, want)
		}
	}

	// An existing query string is joined with '&'.
	joined := dsn("foo.db?cache=shared")
	if !strings.Contains(joined, "foo.db?cache=shared&_txlock=immediate") {
		t.Fatalf("dsn = %q, want '&' separator", joined)
	}
}

func TestOpenInvalidPath(t *testing.T) {
	// A regular file where a directory is expected must fail to open.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if db, err := Open(filepath.Join(blocker, "db.sqlite")); err == nil {
		db.Close()
		t.Fatal("expected Open to fail under a regular file")
	}
}

func TestIsWindowsStoragePath(t *testing.T) {
	for _, input := range []string{"C:/Users/x", "D:/", "//server/share"} {
		if !isWindowsStoragePath(input) {
			t.Fatalf("%q should be a windows storage path", input)
		}
	}
	for _, input := range []string{"/tmp/x", "relative/x", "C:"} {
		if isWindowsStoragePath(input) {
			t.Fatalf("%q should not be a windows storage path", input)
		}
	}
}

func TestAbsolutePathConversions(t *testing.T) {
	for _, convert := range []struct {
		name string
		fn   func(string) (string, error)
		want func(string) string
	}{
		{
			name: "column",
			fn: func(path string) (string, error) {
				return AbsolutePathColumn(schema.AbsolutePath(path))
			},
			// Columns store the slash-form storage path.
			want: storagePath,
		},
		{
			name: "driver",
			fn: func(path string) (string, error) {
				value, err := AbsolutePathFromDriver(path)
				return string(value), err
			},
			// The driver converts back to the platform form.
			want: func(path string) string { return path },
		},
	} {
		t.Run(convert.name, func(t *testing.T) {
			abs := filepath.Join(t.TempDir(), "file")
			got, err := convert.fn(abs)
			if err != nil {
				t.Fatalf("absolute path rejected: %v", err)
			}
			if got != convert.want(abs) {
				t.Fatalf("got %q, want %q", got, convert.want(abs))
			}
			if _, err := convert.fn("relative/file"); err == nil {
				t.Fatal("relative path should be rejected")
			}
		})
	}
}
