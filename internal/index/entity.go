package index

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/henriquesakata/cerne/internal/slug"
)

// titleWeight is the bm25 weight of the title column against 1 for the body
// and the id (F-0367): the title is the summary the author chose.
const titleWeight = 4.0

// maxCoverageTerms caps the per-term MATCH queries of the coverage ranking.
// ponytail: the OR query reads the record of every matching row (~200 ms for
// 3.3k rows on the rockt-hub index); a compact rowid->entity side table would
// cut that if search latency starts to matter.
const maxCoverageTerms = 8

// entityRowsSQL ranks matching documents by bm25 over the columns entity_id,
// kind, doc, state, title, body, superseded_by (index.go createTableSQL).
var entityRowsSQL = fmt.Sprintf(`SELECT rowid, entity_id, kind, doc, state, title, superseded_by
	FROM docs WHERE docs MATCH ? ORDER BY bm25(docs, 1.0, 0, 0, 0, %g, 1.0, 0)`, titleWeight)

const (
	rowidsSQL     = `SELECT rowid FROM docs WHERE docs MATCH ?`
	entitySnipSQL = `SELECT snippet(docs, 5, '«', '»', '…', 12) FROM docs WHERE docs MATCH ? AND rowid = ?`
)

// EntityResult is one entity of a search: its best-ranked document (Result),
// every document of it that matched, and how many distinct query terms its
// documents cover. Partial marks a line of the coverage path: no single
// document of it was required to hold every term.
type EntityResult struct {
	Result
	Docs     []string
	Coverage int
	Partial  bool
	best     int64 // rowid of the best document, for its snippet
}

// SearchEntities is SearchBroad with one line per entity (F-0367). Entities
// with every term in one document (exact, bm25 order) come first. When they
// are fewer than limit, the list is filled by coverage: the keywords ORed with
// their stems, entities ordered by the distinct terms their documents cover
// plus those in their title, then bm25 of the best document (title weighted).
// broadened is true when any line is Partial. Explicit FTS5 syntax and
// single-keyword queries stay exact (F-0343).
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
	res := truncate(groupRows(hits), limit)
	if err := fillSnippets(db, match, res); err != nil {
		return nil, false, err
	}
	terms := keywordTerms(query)
	if len(res) >= limit || ftsSyntax.MatchString(query) || len(terms) < 2 {
		return res, false, nil
	}
	more, err := coverageEntities(db, terms, limit+len(res))
	if err != nil {
		return nil, false, err
	}
	exact := len(res)
	seen := map[string]bool{}
	for _, r := range res {
		seen[r.EntityID] = true
	}
	for _, r := range more {
		if len(res) < limit && !seen[r.EntityID] {
			res = append(res, r)
		}
	}
	return res, len(res) > exact, nil
}

// RankEntities is the coverage ranking of SearchEntities on its own, for a
// natural-language question (cerne ask), whatever punctuation it carries. A
// question with no keyword left falls back to the exact search of the string.
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
	res := truncate(groupRows(hits), limit)
	return res, fillSnippets(db, match, res)
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
// entities by how many distinct terms their documents cover plus how many
// are in their title, bm25 order breaking ties.
func coverageEntities(db *sql.DB, terms []string, limit int) ([]EntityResult, error) {
	if len(terms) > maxCoverageTerms {
		terms = terms[:maxCoverageTerms]
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = termQuery(t)
	}
	match := strings.Join(parts, " OR ")
	hits, err := entityRows(db, match)
	if err != nil {
		return nil, err
	}
	entityOf := make(map[int64]string, len(hits))
	for _, h := range hits {
		entityOf[h.rowid] = h.EntityID
	}
	covered := map[string]map[int]bool{} // entity -> indexes of the terms it covers
	for i, p := range parts {
		if err := markCoverage(db, p, i, entityOf, covered); err != nil {
			return nil, err
		}
	}
	res := groupRows(hits)
	score := make(map[string]int, len(res))
	for i := range res {
		res[i].Coverage = len(covered[res[i].EntityID])
		res[i].Partial = true
		score[res[i].EntityID] = res[i].Coverage + titleHits(res[i].Title, terms)
	}
	sort.SliceStable(res, func(i, j int) bool { return score[res[i].EntityID] > score[res[j].EntityID] })
	res = truncate(res, limit)
	return res, fillSnippets(db, match, res)
}

// titleHits counts the terms whose word, or stem as a prefix, is in title
// (camelCase parts included).
func titleHits(title string, terms []string) int {
	var words []string
	for _, tok := range identifier.FindAllString(title, -1) {
		words = append(words, slug.Fold(tok))
		if parts := splitCamel(tok); len(parts) > 1 {
			for _, p := range parts {
				words = append(words, slug.Fold(p))
			}
		}
	}
	for _, tok := range strings.FieldsFunc(slug.Fold(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		words = append(words, tok)
	}
	n := 0
	for _, t := range terms {
		f := slug.Fold(t)
		s := stem(f)
		for _, w := range words {
			if w == f || (s != f && strings.HasPrefix(w, s)) {
				n++
				break
			}
		}
	}
	return n
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

// markCoverage records term i as covered by the entity of every document
// matching it. Only rowids are read: the entity comes from entityOf, built from
// the OR query that every term's documents are part of.
func markCoverage(db *sql.DB, match string, i int, entityOf map[int64]string, covered map[string]map[int]bool) error {
	rows, err := db.Query(rowidsSQL, match)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rowid int64
		if err := rows.Scan(&rowid); err != nil {
			return err
		}
		id := entityOf[rowid]
		if covered[id] == nil {
			covered[id] = map[int]bool{}
		}
		covered[id][i] = true
	}
	return rows.Err()
}

// docOrder is the display order of a feature's documents in Docs.
var docOrder = map[string]int{"spec": 0, "plan": 1, "result": 2, "errata": 3}

// groupRows groups hits (bm25 order) by entity: the first row of an entity is
// its best document, the others only add to Docs (in docOrder).
func groupRows(hits []rankedRow) []EntityResult {
	var out []EntityResult
	at := map[string]int{}
	for _, h := range hits {
		i, ok := at[h.EntityID]
		if !ok {
			i = len(out)
			at[h.EntityID] = i
			out = append(out, EntityResult{Result: h.Result, Docs: []string{}, best: h.rowid})
		}
		if h.Doc != "" {
			out[i].Docs = append(out[i].Docs, h.Doc)
		}
	}
	for i := range out {
		docs := out[i].Docs
		sort.SliceStable(docs, func(a, b int) bool { return rankDoc(docs[a]) < rankDoc(docs[b]) })
	}
	if out == nil {
		out = []EntityResult{}
	}
	return out
}

func truncate(res []EntityResult, limit int) []EntityResult {
	if limit >= 0 && len(res) > limit {
		return res[:limit]
	}
	return res
}

// fillSnippets sets each result's snippet from its best document for match.
func fillSnippets(db *sql.DB, match string, res []EntityResult) error {
	for i := range res {
		if err := db.QueryRow(entitySnipSQL, match, res[i].best).Scan(&res[i].Snippet); err != nil {
			return err
		}
	}
	return nil
}

func rankDoc(doc string) int {
	if r, ok := docOrder[doc]; ok {
		return r
	}
	return len(docOrder)
}
