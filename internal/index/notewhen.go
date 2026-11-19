package index

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
)

// Declared note triggers (F-0457) live in a plain table next to the FTS docs:
// one row per trigger of an active note. The hooks read every row and match
// in Go — reading the entities from the clone instead cost 46 ms with 1,000
// notes, past the hook budget.
const (
	dropWhenSQL   = `DROP TABLE IF EXISTS note_when`
	createWhenSQL = `CREATE TABLE note_when (entity_id TEXT, title TEXT, trig TEXT)`
)

// errNoWhenTable makes Update fail on an index built before F-0457, so the
// caller falls back to a whole Rebuild.
var errNoWhenTable = errors.New("index predates note triggers; rebuild it")

// TriggerHit is one declared trigger of an active note.
type TriggerHit struct {
	ID, Title, Trig string
}

func insertWhenRows(tx *sql.Tx, rows []row) error {
	stmt, err := tx.Prepare(`INSERT INTO note_when (entity_id, title, trig) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		for _, trig := range r.when {
			if _, err := stmt.Exec(r.entityID, r.title, trig); err != nil {
				return err
			}
		}
	}
	return nil
}

// NoteTriggers returns every declared trigger of the active notes. Like
// PhraseNotes it opens the index read-only with a short busy timeout (a hook
// must never wait on a rebuild); a missing index, or one built before the
// table existed, yields nil, nil.
func NoteTriggers(ctx context.Context, dbPath string) ([]TriggerHit, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&_pragma=busy_timeout(50)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT entity_id, title, trig FROM note_when`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, wrapBusy(err)
	}
	defer rows.Close()
	var out []TriggerHit
	for rows.Next() {
		var h TriggerHit
		if err := rows.Scan(&h.ID, &h.Title, &h.Trig); err != nil {
			return nil, wrapBusy(err)
		}
		out = append(out, h)
	}
	return out, wrapBusy(rows.Err())
}
