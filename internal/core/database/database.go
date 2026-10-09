package database

import (
	"database/sql"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

type Database struct {
	DB *sql.DB
}

// DefaultMaxConns is the connection-pool size used when Open is given no
// explicit size. SQLite serializes writers (every transaction opens with BEGIN
// IMMEDIATE and busy_timeout backs off), while WAL lets reads proceed
// concurrently, so a small pool raises multi-session throughput without
// changing correctness.
const DefaultMaxConns = 4

// Open opens the database. maxConns optionally overrides DefaultMaxConns; a
// non-positive value keeps the default.
func Open(filename string, maxConns ...int) (*Database, error) {
	conns := DefaultMaxConns
	if len(maxConns) > 0 && maxConns[0] > 0 {
		conns = maxConns[0]
	}
	db, err := sql.Open("sqlite", dsn(filename))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(conns)
	db.SetMaxIdleConns(conns)
	// Truncate the WAL on open (a database-wide, connection-independent action).
	if _, err := db.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Database{DB: db}, nil
}

func (d *Database) Close() error {
	return d.DB.Close()
}

// dsn builds the connection string. Per-connection pragmas go in the DSN so
// every pooled connection gets them; _txlock=immediate serializes writers.
func dsn(filename string) string {
	separator := "?"
	if strings.Contains(filename, "?") {
		separator = "&"
	}
	var builder strings.Builder
	builder.WriteString(filename)
	builder.WriteString(separator)
	builder.WriteString("_txlock=immediate")
	for _, pragma := range []string{
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"busy_timeout(5000)",
		"cache_size(-64000)",
		"foreign_keys(1)",
	} {
		builder.WriteString("&_pragma=")
		builder.WriteString(url.QueryEscape(pragma))
	}
	return builder.String()
}
