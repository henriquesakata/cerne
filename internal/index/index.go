// Package index maintains the local SQLite FTS5 full-text index over the
// ENTIRE backend history — superseded and rejected entities included. The
// materialized .docs/ tree shows only current state; this index is how the
// rest is found. It is derived data: drop the file and Rebuild restores it.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // database/sql driver "sqlite" (pure Go, no cgo)

	"gitlab.com/rockt/cerne/internal/store"
)

// featureDocFilenames are the per-feature lifecycle documents indexed as
// individual rows, mirroring the materialize order.
var featureDocFilenames = []string{"spec.md", "plan.md", "result.md"}

const (
	dropTableSQL = `DROP TABLE IF EXISTS docs`
	// entity_id, title and body are searchable; the remaining columns are
	// metadata carried along for display only.
	createTableSQL = `CREATE VIRTUAL TABLE docs USING fts5(
		entity_id, kind UNINDEXED, doc UNINDEXED, state UNINDEXED,
		title, body, superseded_by UNINDEXED)`
	insertSQL = `INSERT INTO docs (entity_id, kind, doc, state, title, body, superseded_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	// 5 is the body column; rank is bm25 with default weights.
	searchSQL = `SELECT entity_id, kind, doc, state, title,
		snippet(docs, 5, '«', '»', '…', 12), superseded_by
		FROM docs WHERE docs MATCH ? ORDER BY rank LIMIT ?`
)

// row is one document to be inserted by Rebuild.
type row struct {
	entityID, kind, doc, state, title, body, supersededBy string
}

// Result is one search hit, ordered by bm25 rank.
type Result struct {
	EntityID     string
	Kind         string // feature | decision | reference
	Doc          string // spec | plan | result | doc | "" (title-only row)
	State        string
	Title        string
	Snippet      string
	SupersededBy string // set on superseded features/decisions
}

// Rebuild drops and recreates the FTS index at dbPath from the backend at
// repoDir. Every entity is indexed regardless of state: features get one row
// per existing lifecycle doc (spec/plan/result), decisions and references one
// row for their content doc; entities without any content doc still get one
// title-only row (doc="", empty body). The whole rebuild runs in a single
// transaction.
func Rebuild(repoDir, dbPath string) error {
	rows, err := collectRows(repoDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(dropTableSQL); err != nil {
		return err
	}
	if _, err := tx.Exec(createTableSQL); err != nil {
		return err
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.entityID, r.kind, r.doc, r.state, r.title, r.body, r.supersededBy); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Search runs an FTS5 MATCH query against the index at dbPath and returns up
// to limit results ordered by bm25 rank. The user string is passed through
// as-is; when fts5 rejects it (special-character syntax error) the query is
// retried quoted as a single phrase.
func Search(dbPath, query string, limit int) ([]Result, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := runQuery(db, query, limit)
	if err != nil {
		// fts5 rejects special characters with assorted messages ("fts5:
		// syntax error", "no such column" for F-0001-style tokens, ...);
		// rather than pattern-match them, retry once with the whole string
		// quoted as a phrase. Non-query errors (missing table, I/O) fail
		// identically on the retry and still surface.
		return runQuery(db, quotePhrase(query), limit)
	}
	return results, nil
}

func runQuery(db *sql.DB, query string, limit int) ([]Result, error) {
	rows, err := db.Query(searchSQL, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []Result{}
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.EntityID, &r.Kind, &r.Doc, &r.State, &r.Title, &r.Snippet, &r.SupersededBy); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// quotePhrase wraps query as one fts5 phrase string, escaping embedded quotes.
func quotePhrase(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

// collectRows loads every entity from the backend and pairs it with its
// content documents.
func collectRows(repoDir string) ([]row, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}

	var rows []row
	for _, f := range features {
		base := row{entityID: f.ID, kind: "feature", state: string(f.State),
			title: f.Title, supersededBy: f.SupersededBy}
		found := false
		for _, filename := range featureDocFilenames {
			body, exists, err := readDoc(filepath.Join(repoDir, "content", "features", f.ID, filename))
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
			r := base
			r.doc = strings.TrimSuffix(filename, ".md")
			r.body = body
			rows = append(rows, r)
			found = true
		}
		if !found {
			rows = append(rows, base)
		}
	}
	for _, d := range decisions {
		r := row{entityID: d.ID, kind: "decision", state: string(d.State),
			title: d.Title, supersededBy: d.SupersededBy}
		body, exists, err := readDoc(filepath.Join(repoDir, "content", "decisions", d.ID+".md"))
		if err != nil {
			return nil, err
		}
		if exists {
			r.doc, r.body = "doc", body
		}
		rows = append(rows, r)
	}
	for _, ref := range references {
		r := row{entityID: ref.ID, kind: "reference", state: string(ref.State), title: ref.Title}
		body, exists, err := readDoc(filepath.Join(repoDir, "content", "references", ref.ID+".md"))
		if err != nil {
			return nil, err
		}
		if exists {
			r.doc, r.body = "doc", body
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// readDoc returns the file content and whether the file exists.
func readDoc(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), true, nil
}
