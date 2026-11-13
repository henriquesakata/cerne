// Package index maintains the local SQLite FTS5 full-text index over the
// ENTIRE backend history — superseded and rejected entities included. The
// materialized .docs/ tree shows only current state; this index is how the
// rest is found. It is derived data: drop the file and Rebuild restores it.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite" (pure Go, no cgo)

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// featureDocFilenames are the per-feature lifecycle documents indexed as
// individual rows, mirroring the materialize order. The errata (F-0367) is one
// more row (doc = "errata"), read from content/errata/<id>.md.
var featureDocFilenames = []string{"spec.md", "plan.md", "result.md"}

const (
	dropTableSQL  = `DROP TABLE IF EXISTS docs`
	dropMetaSQL   = `DROP TABLE IF EXISTS meta`
	createMetaSQL = `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`
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
func Rebuild(repoDir, dbPath string, syncedAt time.Time) error {
	head := cloneHead(repoDir)
	rows, err := collectRows(repoDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := openDB(dbPath)
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
	if _, err := tx.Exec(dropMetaSQL); err != nil {
		return err
	}
	if _, err := tx.Exec(createMetaSQL); err != nil {
		return err
	}
	if !syncedAt.IsZero() {
		if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('synced_at', ?)`, syncedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if err := insertRows(tx, rows); err != nil {
		return err
	}
	if err := setHead(tx, head); err != nil {
		return err
	}
	return tx.Commit()
}

// cloneHead is the clone's HEAD sha, "" when it cannot be read (a test tree
// that is not a git repo): the index then records no head.
func cloneHead(repoDir string) string {
	h, err := gitback.HeadSHA(repoDir)
	if err != nil {
		return ""
	}
	return h
}

// setHead records the clone head the index now reflects, in the caller's
// transaction (the same one that wrote synced_at).
func setHead(tx *sql.Tx, head string) error {
	if head == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('indexed_head', ?)`, head)
	return err
}

// IndexedHead is the clone head the index at dbPath last indexed; ok is false
// when none is recorded (the caller then rebuilds whole). A failed refresh
// leaves it at the old value, so the next one re-covers the gap.
func IndexedHead(dbPath string) (head string, ok bool) {
	db, err := openDB(dbPath)
	if err != nil {
		return "", false
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'indexed_head'`).Scan(&head); err != nil {
		return "", false
	}
	return head, head != ""
}

func insertRows(tx *sql.Tx, rows []row) error {
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.entityID, r.kind, r.doc, r.state, r.title, foldIdentifiers(r.body), r.supersededBy); err != nil {
			return err
		}
	}
	return nil
}

// Update re-indexes only the entities in ids: their rows are deleted and
// reinserted from the clone at repoDir, in one transaction; an id the clone no
// longer has is only deleted. It needs an index Rebuild already built (an
// error otherwise: the caller falls back to Rebuild). syncedAt, when not zero,
// replaces the recorded sync time. The clone is read whole and filtered
// (~0.4 s on the hub), the FTS writes are the part that stays proportional to
// ids — ponytail: load only the touched entities if the read ever matters.
func Update(repoDir, dbPath string, ids []string, syncedAt time.Time) error {
	if _, err := os.Stat(dbPath); err != nil {
		return err
	}
	head := cloneHead(repoDir)
	all, err := collectRows(repoDir)
	if err != nil {
		return err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var rows []row
	for _, r := range all {
		if want[r.entityID] {
			rows = append(rows, r)
		}
	}
	db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id := range want {
		if _, err := tx.Exec(`DELETE FROM docs WHERE entity_id = ?`, id); err != nil {
			return err
		}
	}
	if err := insertRows(tx, rows); err != nil {
		return err
	}
	if !syncedAt.IsZero() {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('synced_at', ?)`, syncedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if err := setHead(tx, head); err != nil {
		return err
	}
	return tx.Commit()
}

// IDsFromPaths maps backend paths (as `git diff --name-only` prints them) to
// the entity ids they belong to: entities/<kind>/<id>.json,
// content/features/<id>/<doc>.md and content/<kind>/<id>.md (errata included).
// Other paths map to nothing; the result is sorted and de-duplicated.
func IDsFromPaths(paths []string) []string {
	seen := map[string]bool{}
	for _, p := range paths {
		parts := strings.Split(p, "/")
		stem := func(s string) string { return strings.TrimSuffix(s, filepath.Ext(s)) }
		switch {
		case len(parts) == 3 && parts[0] == "entities" && strings.HasSuffix(parts[2], ".json"):
			seen[stem(parts[2])] = true
		case len(parts) == 4 && parts[0] == "content" && parts[1] == "features":
			seen[parts[2]] = true
		case len(parts) == 3 && parts[0] == "content" && strings.HasSuffix(parts[2], ".md"):
			seen[stem(parts[2])] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// Search runs an FTS5 MATCH query against the index at dbPath and returns up
// to limit results ordered by bm25 rank. The user string is passed through
// as-is; when fts5 rejects it (special-character syntax error) the query is
// retried quoted as a single phrase.
func Search(dbPath, query string, limit int) ([]Result, error) {
	db, err := openDB(dbPath)
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

// ftsSyntax matches FTS5 query syntax: quotes, prefix star, grouping, column
// filter, and the uppercase boolean operators.
var ftsSyntax = regexp.MustCompile(`["*():^+]|\b(AND|OR|NOT|NEAR)\b`)

// SearchBroad is Search, but when no document has all the terms it retries
// with the query's keywords OR-joined (bm25 order) and reports broadened=true.
// It does not retry when the query already uses FTS5 syntax or has fewer than
// two keywords (F-0343).
func SearchBroad(dbPath, query string, limit int) ([]Result, bool, error) {
	results, err := Search(dbPath, query, limit)
	if err != nil || len(results) > 0 || ftsSyntax.MatchString(query) || len(keywordTerms(query)) < 2 {
		return results, false, err
	}
	results, err = Search(dbPath, Keywords(query), limit)
	return results, len(results) > 0, err
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
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	historicals, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		return nil, err
	}

	var rows []row
	for _, f := range features {
		base := row{entityID: f.ID, kind: "feature", state: string(f.State),
			title: f.Title, supersededBy: f.SupersededBy}
		// Index the tags (D-0003) so a search by facet/component finds the feature
		// even when the term never appears in its prose (mirrors decisions/notes).
		tags := strings.Join(append(append([]string{}, f.Facets...), f.Components...), " ")
		if tags != "" {
			base.body = tags // carried when the feature has no content doc yet
		}
		found := false
		paths := make([]string, 0, len(featureDocFilenames)+1)
		for _, filename := range featureDocFilenames {
			paths = append(paths, filepath.Join(repoDir, "content", "features", f.ID, filename))
		}
		paths = append(paths, filepath.Join(repoDir, "content", "errata", f.ID+".md"))
		for _, path := range paths {
			body, exists, err := readDoc(path)
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
			r := base
			r.doc = strings.TrimSuffix(filepath.Base(path), ".md")
			if r.doc == f.ID {
				r.doc = "errata"
			}
			r.body = strings.TrimSpace(body + " " + tags)
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
		// Index the tags into the body so a search by facet/component finds the
		// decision even when the term never appears in its prose (mirrors notes).
		if tags := strings.Join(append(append([]string{}, d.Facets...), d.Components...), " "); tags != "" {
			r.body = strings.TrimSpace(r.body + " " + tags)
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
		// Index the tags into the body so a search by facet/component finds the
		// reference even when the term never appears in its prose (mirrors notes).
		if tags := strings.Join(append(append([]string{}, ref.Facets...), ref.Components...), " "); tags != "" {
			r.body = strings.TrimSpace(r.body + " " + tags)
		}
		rows = append(rows, r)
	}
	for _, rb := range runbooks {
		r := row{entityID: rb.ID, kind: "runbook", state: string(rb.State),
			title: rb.Title, supersededBy: rb.SupersededBy}
		body, exists, err := readDoc(filepath.Join(repoDir, "content", "runbooks", rb.ID+".md"))
		if err != nil {
			return nil, err
		}
		if exists {
			r.doc, r.body = "doc", body
		}
		rows = append(rows, r)
	}
	for _, n := range notes {
		r := row{entityID: n.ID, kind: "note", state: string(n.State),
			title: n.Title, supersededBy: n.SupersededBy}
		body, exists, err := readDoc(filepath.Join(repoDir, "content", "notes", n.ID+".md"))
		if err != nil {
			return nil, err
		}
		if exists {
			r.doc, r.body = "doc", body
		}
		// Index the tags into the body so a search by facet/component finds the
		// note even when the term never appears in its prose.
		if tags := strings.Join(append(append([]string{}, n.Facets...), n.Components...), " "); tags != "" {
			r.body = strings.TrimSpace(r.body + " " + tags)
		}
		rows = append(rows, r)
	}
	for _, h := range historicals {
		// Historical entities have no content doc; their aliases and reason
		// form the searchable body so the index is the door to them.
		body := strings.TrimSpace(strings.Join(h.Aliases, " ") + " " + h.Reason)
		rows = append(rows, row{entityID: h.ID, kind: "historical", state: string(h.State),
			title: h.Name, body: body, supersededBy: h.SupersededBy})
	}
	return rows, nil
}

// identifier matches a run of letters/digits that starts with a letter.
var identifier = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)

// foldIdentifiers appends, once per distinct identifier, the parts of every
// camelCase/PascalCase identifier in body ("WorkspaceSwitcher" -> "Workspace
// Switcher", "parseHTTPRequest" -> "parse HTTP Request"), so a search for one
// part finds the document (F-0367). The unicode61 tokenizer keeps such an
// identifier as one token. Letter-digit runs ("F0396") are not split.
func foldIdentifiers(body string) string {
	var parts []string
	seen := map[string]bool{}
	for _, tok := range identifier.FindAllString(body, -1) {
		split := splitCamel(tok)
		if len(split) < 2 || seen[tok] {
			continue
		}
		seen[tok] = true
		parts = append(parts, strings.Join(split, " "))
	}
	if len(parts) == 0 {
		return body
	}
	return body + "\n" + strings.Join(parts, "\n")
}

// splitCamel splits tok before an upper-case letter that follows a lower-case
// letter or a digit ("getV2Client" -> "get", "V2", "Client"), and before the
// last upper of an upper run followed by a lower ("HTTPRequest" -> "HTTP",
// "Request").
func splitCamel(tok string) []string {
	r := []rune(tok)
	isUp := func(c rune) bool { return c >= 'A' && c <= 'Z' }
	isLow := func(c rune) bool { return c >= 'a' && c <= 'z' }
	isDigit := func(c rune) bool { return c >= '0' && c <= '9' }
	var out []string
	start := 0
	for i := 1; i < len(r); i++ {
		if isUp(r[i]) && (isLow(r[i-1]) || isDigit(r[i-1]) || (isUp(r[i-1]) && i+1 < len(r) && isLow(r[i+1]))) {
			out = append(out, string(r[start:i]))
			start = i
		}
	}
	return append(out, string(r[start:]))
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

// openDB opens dbPath waiting up to 5 s on a lock held by another writer, so
// two agents writing at once do not turn into a failed refresh (F-0394).
// _txlock=immediate takes the write lock at BEGIN: a deferred read-then-write
// upgrade fails SQLITE_BUSY at once without consulting the busy handler.
func openDB(dbPath string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: dbPath, RawQuery: "_pragma=busy_timeout(5000)&_txlock=immediate"}
	return sql.Open("sqlite", u.String())
}

// SyncedAt returns when the index at dbPath was last synced with the backend.
// ok is false when the index records no time (built lazily by a first search,
// or built before F-0394); err is only for a real read failure.
func SyncedAt(dbPath string) (t time.Time, ok bool, err error) {
	db, err := openDB(dbPath)
	if err != nil {
		return t, false, err
	}
	defer db.Close()
	var v string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = 'synced_at'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no such table")) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	t, err = time.Parse(time.RFC3339, v)
	return t, err == nil, err
}
