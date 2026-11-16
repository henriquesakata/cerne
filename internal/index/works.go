package index

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/store"
)

// Works live in their own FTS table, so adding them never moves the bm25
// statistics (and the order) of the docs table (F-0408).
const (
	dropWorksSQL   = `DROP TABLE IF EXISTS works_fts`
	createWorksSQL = `CREATE VIRTUAL TABLE works_fts USING fts5(
		work_id, state UNINDEXED, actor UNINDEXED, raw_title UNINDEXED, title, body)`
	insertWorkSQL = `INSERT INTO works_fts (work_id, state, actor, raw_title, title, body) VALUES (?, ?, ?, ?, ?, ?)`
	searchWorkSQL = `SELECT work_id, state, actor, raw_title FROM works_fts
		WHERE works_fts MATCH ? ORDER BY (state = 'active') DESC, bm25(works_fts, 1.0, 0, 0, 0, 10.0, 1.0), work_id LIMIT ?`
)

// workRow is one work to insert: body joins the closing note, the handoff and
// the log entries' texts.
type workRow struct {
	id, state, actor, title, body string
}

// WorkResult is one work found by SearchWorks.
type WorkResult struct {
	ID, State, Actor, Title string
}

// workRowOf reads a work's handoff and log. logged, when not nil, is the set of
// works that have a log directory: the rest skip the lookup.
func workRowOf(repoDir string, w store.Work, logged map[string]bool) (workRow, error) {
	handoff, _, err := readDoc(filepath.Join(repoDir, "content", "works", w.ID, "handoff.md"))
	if err != nil {
		return workRow{}, err
	}
	var log []store.LogEntry
	if logged == nil || logged[w.ID] {
		var err error
		if log, err = store.LoadLog(repoDir, w.ID); err != nil {
			return workRow{}, err
		}
	}
	parts := []string{w.Note, store.LabelHandoffHeaders(handoff)}
	for _, e := range log {
		parts = append(parts, e.Text)
	}
	return workRow{w.ID, string(w.State), w.Actor, w.Title, strings.TrimSpace(strings.Join(parts, "\n"))}, nil
}

func collectWorkRows(repoDir string) ([]workRow, error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}
	// One listing instead of one failed lookup per work with no log.
	logged := map[string]bool{}
	dirs, err := os.ReadDir(filepath.Join(repoDir, "content", "worklogs"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, d := range dirs {
		logged[d.Name()] = true
	}
	rows := make([]workRow, 0, len(works))
	for _, w := range works {
		r, err := workRowOf(repoDir, w, logged)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// collectWorkRowsFor is collectWorkRows restricted to the W- ids in ids.
func collectWorkRowsFor(repoDir string, ids []string) ([]workRow, error) {
	var rows []workRow
	for _, id := range slices.Sorted(slices.Values(ids)) {
		if !strings.HasPrefix(id, "W-") {
			continue
		}
		w, err := store.LoadWork(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		r, err := workRowOf(repoDir, w, nil)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func insertWorkRows(tx *sql.Tx, rows []workRow) error {
	stmt, err := tx.Prepare(insertWorkSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.id, r.state, r.actor, r.title, foldIdentifiers(r.title), foldIdentifiers(r.body)); err != nil {
			return err
		}
	}
	return nil
}

// SearchWorks finds works matching query and returns up to limit of them with
// the total that match. Active works come first, then bm25 with the title
// weighted far above the note, handoff and log. Plain words are prefix terms
// ("rota" finds "rotação"): works with every term first, then, when none has
// them all, works with any. FTS5 syntax in the query is passed as-is, retried
// quoted on a syntax error. An index built before works were indexed has no
// table: that is no works, not an error.
func SearchWorks(dbPath, query string, limit int) (works []WorkResult, total int, err error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, 0, err
	}
	db, err := openDB(dbPath)
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	// Try each form in turn; a syntax error moves to the next form, and only
	// when every form errors is the error returned.
	var lastErr error
	ok := false
	for _, m := range workMatches(query) {
		w, n, qerr := runWorkQuery(db, m, limit)
		if qerr != nil {
			lastErr = qerr
			continue
		}
		ok = true
		works, total = w, n
		if n > 0 {
			break
		}
	}
	err = nil
	if !ok {
		err = lastErr
	}
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return nil, 0, nil
	}
	return works, total, err
}

// workMatches is the MATCH expressions to try, in order.
func workMatches(query string) []string {
	terms := keywordTerms(query)
	if ftsSyntax.MatchString(query) || len(terms) == 0 {
		return []string{query, quotePhrase(query)}
	}
	for i, t := range terms {
		terms[i] = quotePhrase(t) + "*"
	}
	out := []string{strings.Join(terms, " ")}
	if len(terms) > 1 {
		out = append(out, strings.Join(terms, " OR "))
	}
	return append(out, quotePhrase(query))
}

func runWorkQuery(db *sql.DB, match string, limit int) ([]WorkResult, int, error) {
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM works_fts WHERE works_fts MATCH ?`, match).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(searchWorkSQL, match, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []WorkResult
	for rows.Next() {
		var r WorkResult
		if err := rows.Scan(&r.ID, &r.State, &r.Actor, &r.Title); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// HasWorks reports whether the index at dbPath has the works table; an index
// built before works were indexed (or with an older works schema) does not
// and must be rebuilt once.
func HasWorks(dbPath string) bool {
	db, err := openDB(dbPath)
	if err != nil {
		return false
	}
	defer db.Close()
	var n int
	return db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'works_fts' AND sql LIKE '%raw_title%'`).Scan(&n) == nil && n > 0
}
