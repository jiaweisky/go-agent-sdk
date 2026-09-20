package session

import "database/sql"

// DB is the narrow database surface the session package needs. Both
// *sql.DB and the event transaction (core.EventDB) satisfy it, so projectors
// can run their writes inside the event transaction.
type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}
