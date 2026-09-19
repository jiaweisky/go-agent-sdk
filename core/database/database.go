package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Database struct {
	DB *sql.DB
}

func Open(filename string) (*Database, error) {
	db, err := sql.Open("sqlite", withImmediateTx(filename))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -64000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA wal_checkpoint(PASSIVE)",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
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

func withImmediateTx(filename string) string {
	separator := "?"
	if strings.Contains(filename, "?") {
		separator = "&"
	}
	return filename + separator + "_txlock=immediate"
}

func Path() string {
	if db := os.Getenv("OPENCODE_DB"); db != "" {
		if db == ":memory:" || filepath.IsAbs(db) {
			return db
		}
		return filepath.Join(globalDataDir(), db)
	}
	channel := InstallationChannel()
	if channel == "latest" || channel == "beta" || channel == "prod" ||
		os.Getenv("OPENCODE_DISABLE_CHANNEL_DB") == "1" ||
		os.Getenv("OPENCODE_DISABLE_CHANNEL_DB") == "true" {
		return filepath.Join(globalDataDir(), "opencode.db")
	}
	return filepath.Join(globalDataDir(), "opencode-"+sanitizeChannel(channel)+".db")
}

func globalDataDir() string {
	if dir := os.Getenv("OPENCODE_DATA"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share", "opencode")
}

func InstallationChannel() string {
	if channel := os.Getenv("OPENCODE_CHANNEL"); channel != "" {
		return channel
	}
	return "latest"
}

func sanitizeChannel(channel string) string {
	var b strings.Builder
	for _, r := range channel {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
