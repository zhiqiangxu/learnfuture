package model

import "database/sql"

// Executor is an interface satisfied by both *sql.DB and *sql.Tx.
type Executor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}
