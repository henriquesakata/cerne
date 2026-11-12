package index

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// titleWeight is the bm25 weight of the title column against 1 for the body
// and the id (F-0367): the title is the summary the author chose.
const titleWeight = 4.0

// maxCoverageTerms caps the per-term MATCH queries of the coverage ranking.
const maxCoverageTerms = 8

// entityRowsSQL ranks matching documents by bm25 over the columns entity_id,
// kind, doc, state, title, body, superseded_by (index.go createTableSQL).
var entityRowsSQL = fmt.Sprintf(`SELECT rowid, entity_id, kind, doc, state, title, superseded_by
	FROM docs WHERE docs MATCH ? ORDER BY bm25(docs, 1.0, 0, 0, 0, %g, 1.0, 0)`, titleWeight)

const (
	entityIDsSQL  = `SELECT DISTINCT entity_id FROM docs WHERE docs MATCH ?`
	entitySnipSQL = `SELECT snippet(docs, 5, '«', '»', '…', 12) FROM docs WHERE docs MATCH ? AND rowid = ?`
)

// EntityResult is one entity of a search: its best-ranked document (Result),
// every document of it that matched, and how many distinct query terms its
// documents cover (set on the broadened path only).
type EntityResult struct {
	Result
	Docs     []string
	Coverage int
}

// SearchEntities is SearchBroad with one line per entity (F-0367). It first
// tries every term in one document (exact); when that finds nothing it ORs the
// keywords with their stems (Keywords) and orders entities by the number of
// distinct terms their documents cover, bm25 of the best document (title
// weighted) breaking ties, and reports broadened=true. Explicit FTS5 syntax
// and single-keyword queries are not broadened (F-0343).
func SearchEntities(dbPath, query string, limit int) ([]EntityResult, bool, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, false, err
	}
	defer db.Close()

	hits, match, err := exactRows(db, query)
	if err != nil {
		return nil, false, err
	}
	terms := keywordTerms(query)
	if len(hits) > 0 || ftsSyntax.MatchString(query) || len(terms) < 2 {
		res, err := topEntities(db, match, hits, nil, limit)
		return res, false, err
	}
	res, err := coverageEntities(db, terms, limit)
	return res, len(res) > 0, err
}

// RankEntities is the broadened path of SearchEntities on its own, for a
// natural-language question (cerne ask): the keywords' coverage ranking,
// whatever punctuation the question carries. A question with no keyword left
// falls back to the exact search of the whole string.
func RankEntities(dbPath, query string, limit int) ([]EntityResult, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if terms := keywordTerms(query); len(terms) > 0 {
		return coverageEntities(db, terms, limit)
	}
	hits, match, err := exactRows(db, query)
	if err != nil {
		return nil, err
	}
	return topEntities(db, match, hits, nil, limit)
}

// exactRows matches query as given, retried as one quoted phrase when FTS5
// rejects it (the Search fallback); it returns the MATCH string that worked.
func exactRows(db *sql.DB, query string) ([]rankedRow, string, error) {
	if hits, err := entityRows(db, query); err == nil {
		return hits, query, nil
	}
	match := quotePhrase(query)
	hits, err := entityRows(db, match)
	return hits, match, err
}

// coverageEntities ORs terms (each with its stem prefix) and orders the
// entities by how many distinct terms their documents cover.
func coverageEntities(db *sql.DB, terms []string, limit int) ([]EntityResult, error) {
	if len(terms) > maxCoverageTerms {
		terms = terms[:maxCoverageTerms]
	}
	coverage := map[string]int{}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = termQuery(t)
		if err := countCoverage(db, parts[i], coverage); err != nil {
			return nil, err
		}
	}
	match := strings.Join(parts, " OR ")
	hits, err := entityRows(db, match)
	if err != nil {
		return nil, err
	}
	return topEntities(db, match, hits, coverage, limit)
}

// rankedRow is a matching document in bm25 order, without its snippet.
type rankedRow struct {
	rowid int64
	Result
}

func entityRows(db *sql.DB, match string) ([]rankedRow, error) {
	rows, err := db.Query(entityRowsSQL, match)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rankedRow
	for rows.Next() {
		var r rankedRow
		if err := rows.Scan(&r.rowid, &r.EntityID, &r.Kind, &r.Doc, &r.State, &r.Title, &r.SupersededBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func countCoverage(db *sql.DB, match string, coverage map[string]int) error {
	rows, err := db.Query(entityIDsSQL, match)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		coverage[id]++
	}
	return rows.Err()
}

// docOrder is the display order of a feature's documents in Docs.
var docOrder = map[string]int{"spec": 0, "plan": 1, "result": 2, "errata": 3}

// topEntities groups hits (bm25 order) by entity — the first row of an entity
// is its best document — orders the entities by coverage (stable, so bm25
// order survives ties), keeps limit of them and fetches their snippets.
func topEntities(db *sql.DB, match string, hits []rankedRow, coverage map[string]int, limit int) ([]EntityResult, error) {
	out := []EntityResult{}
	best := map[string]int64{}
	at := map[string]int{}
	for _, h := range hits {
		i, ok := at[h.EntityID]
		if !ok {
			i = len(out)
			at[h.EntityID] = i
			best[h.EntityID] = h.rowid
			out = append(out, EntityResult{Result: h.Result, Coverage: coverage[h.EntityID]})
		}
		if h.Doc != "" {
			out[i].Docs = append(out[i].Docs, h.Doc)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Coverage > out[j].Coverage })
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		docs := out[i].Docs
		sort.SliceStable(docs, func(a, b int) bool { return rankDoc(docs[a]) < rankDoc(docs[b]) })
		if err := db.QueryRow(entitySnipSQL, match, best[out[i].EntityID]).Scan(&out[i].Snippet); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func rankDoc(doc string) int {
	if r, ok := docOrder[doc]; ok {
		return r
	}
	return len(docOrder)
}
