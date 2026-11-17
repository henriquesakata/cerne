package index

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"slices"

	"github.com/henriquesakata/cerne/internal/slug"
)

// TitleHit is one active note or runbook whose title the prompt matched.
type TitleHit struct {
	ID, Title string
}

// promptStopwords are words a prompt carries that the search stopwords keep:
// pronouns, auxiliaries and filler ("help me", "preciso", "isso"). Folded.
var promptStopwords = map[string]bool{
	"isso": true, "esse": true, "essa": true, "este": true, "esta": true, "meu": true, "minha": true,
	"nao": true, "mais": true, "mas": true, "pode": true, "posso": true, "preciso": true, "quero": true,
	"fazer": true, "faco": true, "ajuda": true, "ajudar": true, "favor": true, "ainda": true, "ate": true,
	"depois": true, "antes": true, "apos": true, "dando": true, "tem": true, "ter": true, "uma": true,
	"you": true, "can": true, "this": true, "that": true, "my": true, "not": true, "please": true,
	"help": true, "need": true, "does": true, "doing": true, "has": true, "have": true, "from": true,
	"after": true, "before": true, "but": true, "its": true, "it": true, "me": true, "we": true,
}

const (
	// promptWindow is how much of a prompt is read, in runes: the question
	// sits in the first lines, and what follows is usually a pasted log or
	// code whose every word would otherwise match some title by chance.
	promptWindow = 500
	// pasteTerms is the number of significant terms above which the prompt
	// is a paste, not a question: a title must then be (almost) all there.
	pasteTerms = 15
)

// stemSet is the folded, stemmed significant terms of text.
func stemSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, t := range keywordTerms(text) {
		f := slug.Fold(t)
		if promptStopwords[f] || stopwords[f] {
			continue
		}
		set[stem(f)] = true
	}
	return set
}

// PromptTitles returns up to limit active notes and runbooks whose TITLE the
// prompt matches (B-0020): precision over recall, so a title needs at least 2
// significant terms, and the prompt must carry at least 2 of them and at
// least half of them. Best match first. The index is opened read-only with a
// short busy timeout; a missing index yields nil, nil. Nothing is written or
// rebuilt.
func PromptTitles(ctx context.Context, dbPath, prompt string, limit int) ([]TitleHit, error) {
	if r := []rune(prompt); len(r) > promptWindow {
		prompt = string(r[:promptWindow])
	}
	want := stemSet(prompt)
	if len(want) < 2 || limit <= 0 {
		return nil, nil
	}
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
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT entity_id, kind, title FROM docs WHERE kind IN ('note','runbook') AND state = 'active' ORDER BY entity_id LIMIT 5000`)
	if err != nil {
		return nil, wrapBusy(err)
	}
	defer rows.Close()
	type scored struct {
		hit          TitleHit
		matched, all int
	}
	var found []scored
	listed := map[string]bool{}
	for rows.Next() {
		var id, kind, title string
		if err := rows.Scan(&id, &kind, &title); err != nil {
			return nil, wrapBusy(err)
		}
		if listed[id] {
			continue
		}
		listed[id] = true
		terms := stemSet(title)
		matched := 0
		for t := range terms {
			if want[t] {
				matched++
			}
		}
		enough := matched*2 >= len(terms)
		if len(want) > pasteTerms {
			enough = matched >= 3 && matched*4 >= len(terms)*3
		}
		if len(terms) >= 2 && matched >= 2 && enough {
			found = append(found, scored{TitleHit{id, title}, matched, len(terms)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapBusy(err)
	}
	slices.SortFunc(found, func(a, b scored) int {
		switch {
		case a.matched != b.matched:
			return b.matched - a.matched
		case a.matched*b.all != b.matched*a.all: // higher matched/all first
			return b.matched*a.all - a.matched*b.all
		case a.hit.ID < b.hit.ID:
			return -1
		default:
			return 1
		}
	})
	var out []TitleHit
	for _, s := range found[:min(limit, len(found))] {
		out = append(out, s.hit)
	}
	return out, nil
}
