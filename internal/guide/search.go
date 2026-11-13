package guide

import (
	"database/sql"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/slug"
)

// maxText is the longest section text a result carries, ellipsis included.
const maxText = 1200

// maxTerms caps the per-term MATCH queries, as in the entity ranking.
const maxTerms = 8

// Search ranks sections against query in an in-memory FTS5 index and returns
// the best limit of them, texts cut to maxText. Ordering mirrors the entity
// coverage ranking (F-0367): a section covering more of the question's terms,
// counting those in its title again, comes first; bm25 breaks ties. Nothing
// matching, or a blank query, returns nil.
func Search(sections []Section, query string, limit int) []Section {
	terms := index.Terms(query)
	if len(terms) == 0 {
		terms = fallbackTerms(query)
	}
	if len(terms) == 0 || len(sections) == 0 {
		return nil
	}
	if len(terms) > maxTerms {
		terms = terms[:maxTerms]
	}
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		return nil
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // every connection to :memory: is its own database
	if _, err := db.Exec(`CREATE VIRTUAL TABLE s USING fts5(title, text)`); err != nil {
		return nil
	}
	for i, sec := range sections {
		if _, err := db.Exec(`INSERT INTO s (rowid, title, text) VALUES (?, ?, ?)`, i, sec.Title, sec.Text); err != nil {
			return nil
		}
	}
	matches := make([]string, len(terms))
	for i, t := range terms {
		matches[i] = t.Match
	}
	order, err := rowids(db, `SELECT rowid FROM s WHERE s MATCH ? ORDER BY bm25(s, 3.0, 1.0)`, strings.Join(matches, " OR "))
	if err != nil || len(order) == 0 {
		return nil
	}
	score := map[int]int{}
	for _, m := range matches {
		ids, err := rowids(db, `SELECT rowid FROM s WHERE s MATCH ?`, m)
		if err != nil {
			return nil
		}
		for _, id := range ids {
			score[id]++
		}
	}
	for _, id := range order {
		score[id] += titleHits(sections[id].Title, terms)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if score[a] != score[b] {
			return score[a] > score[b]
		}
		return sourceRank(sections[a].Source) < sourceRank(sections[b].Source) // equal coverage: bm25 order (stable) decides within a source
	})
	if limit > 0 && len(order) > limit {
		order = order[:limit]
	}
	out := make([]Section, len(order))
	for i, id := range order {
		out[i] = sections[id]
		out[i].Text = cut(out[i].Text)
	}
	return out
}

func rowids(db *sql.DB, q, match string) ([]int, error) {
	rows, err := db.Query(q, match)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// fallbackTerms serves a query whose words are all too short or stopwords
// (e.g. "ui"): every word of two letters or more that is no stopword is a term, quoted so it never reaches MATCH as syntax.
func fallbackTerms(query string) []index.Term {
	var out []index.Term
	for _, w := range strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		f := slug.Fold(w)
		if utf8.RuneCountInString(f) < 2 || index.IsStopword(f) {
			continue
		}
		out = append(out, index.Term{Word: f, Stem: f, Match: `"` + strings.ReplaceAll(w, `"`, `""`) + `"`})
	}
	return out
}

// titleHits counts the terms whose word, or stem as a prefix, is a word of title.
func titleHits(title string, terms []index.Term) int {
	words := strings.FieldsFunc(slug.Fold(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	n := 0
	for _, t := range terms {
		for _, w := range words {
			if w == t.Word || (t.Stem != t.Word && strings.HasPrefix(w, t.Stem)) {
				n++
				break
			}
		}
	}
	return n
}

// sourceRank orders equal-coverage sections: the FAQ answers a question
// directly, the skill explains the concept, the command help is the reference.
func sourceRank(source string) int {
	switch {
	case source == "faq":
		return 0
	case strings.HasPrefix(source, "skill: "):
		return 1
	}
	return 2
}

// cut shortens text to maxText runes, ending in an ellipsis when it cuts.
func cut(text string) string {
	if utf8.RuneCountInString(text) <= maxText {
		return text
	}
	return string([]rune(text)[:maxText-1]) + "…"
}
