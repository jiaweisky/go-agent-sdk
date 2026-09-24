package database

import (
	"database/sql"
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
