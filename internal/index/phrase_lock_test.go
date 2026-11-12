package index

import (
	"database/sql"
	"testing"
)

// lockIndex takes an EXCLUSIVE lock on the index (a concurrent rebuild) and
// returns the release func.
func lockIndex(t *testing.T, dbPath string) func() {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	return func() { _, _ = db.Exec("ROLLBACK"); db.Close() }
}
