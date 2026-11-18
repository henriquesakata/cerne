package index

import (
	"cmp"
	"context"
	"database/sql"
	"math"
	"net/url"
	"os"
	"slices"
	"strings"

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
	"dando": true, "tem": true, "ter": true, "uma": true,
	"you": true, "can": true, "this": true, "that": true, "my": true, "not": true, "please": true,
	"help": true, "need": true, "does": true, "doing": true, "has": true, "have": true, "from": true,
	"but": true, "its": true, "it": true, "me": true, "we": true,
	// Accessor prefixes a split identifier leaves behind (getUserName ->
	// "get"): they name no subject (B-0028).
	"get": true, "set": true,
}

const (
	// promptWindow is how much of a prompt is read, in runes: the question
	// sits in the first lines, and what follows is usually a pasted log or
	// code whose every word would otherwise match some title by chance.
	promptWindow = 500
	// pasteTerms is the number of significant terms above which the prompt
	// is a paste, not a question: a title must then be (almost) all there.
	pasteTerms = 15
	// minCorpus is the fewest titles rarity is measured against (B-0026).
	minCorpus = 20
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
// prompt matches (B-0020). A natural prompt names 2 to 4 terms of a
// descriptive title, never half of it (B-0026), so a title fires on at least
// 2 shared terms whose rarity among the active titles adds up to at least one
// term held by a single title: two words most titles share never fire alone.
// A paste (more than pasteTerms terms) matches some title by chance, so it
// fires only on (almost) the whole title, or on its first line read as the
// question. Rarest match first. The index is opened read-only with a short
// busy timeout; a missing index yields nil, nil. Nothing is written or
// rebuilt.
func PromptTitles(ctx context.Context, dbPath, prompt string, limit int) ([]TitleHit, error) {
	if r := []rune(prompt); len(r) > promptWindow {
		prompt = string(r[:promptWindow])
	}
	// A question naming a class's parts ("the date serializer") must meet the
	// title's JsonDateSerializer, so questions and titles split identifiers
	// (B-0028). A paste keeps them whole: code split into words matches some
	// title by chance (context.WithTimeout -> "timeout").
	want := stemSet(prompt)
	paste := len(want) > pasteTerms
	if !paste {
		want = stemSet(foldIdentifiers(prompt))
	}
	if len(want) < 2 || limit <= 0 {
		return nil, nil
	}
	var head map[string]bool
	if paste {
		first, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
		if len(stemSet(first)) <= pasteTerms {
			head = stemSet(foldIdentifiers(first))
		}
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
	type titled struct {
		hit   TitleHit
		terms map[string]bool // identifiers split (B-0028)
		whole map[string]bool // identifiers whole, as a paste is read
	}
	var titles []titled
	df := map[string]int{} // titles holding each term
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
		terms := stemSet(foldIdentifiers(title))
		for t := range terms {
			df[t]++
		}
		titles = append(titles, titled{TitleHit{id, title}, terms, stemSet(title)})
	}
	if err := rows.Err(); err != nil {
		return nil, wrapBusy(err)
	}
	// shared counts the title terms in set and sums their rarity: 1 for a
	// term one title holds, falling to 0 as every title holds it (IDF
	// normalized by the number of titles, so the bar holds at 20 notes or
	// 600). Under minCorpus titles no word is common yet: a handful of notes
	// on one subject must all still fire.
	logN := math.Log(float64(max(len(titles), minCorpus) + 1))
	shared := func(terms, set map[string]bool) (n int, rarity float64) {
		for t := range terms {
			if set[t] {
				n++
				rarity += 1 - math.Log(float64(df[t]))/logN
			}
		}
		return n, rarity
	}
	type scored struct {
		hit    TitleHit
		rarity float64
	}
	var found []scored
	for _, ti := range titles {
		n, rarity := shared(ti.terms, want)
		ok := n >= 2 && rarity >= 1
		if paste {
			// The whole-title share counts unsplit titles against the
			// unsplit paste: split parts would only inflate the title.
			n, _ = shared(ti.whole, want)
			hn, hr := shared(ti.terms, head)
			ok = n >= 3 && n*4 >= len(ti.whole)*3 || hn >= 2 && hr >= 1
		}
		if ok {
			found = append(found, scored{ti.hit, rarity})
		}
	}
	slices.SortFunc(found, func(a, b scored) int {
		// Equal term sets sum in map order: a last-bit difference is a tie.
		if math.Abs(a.rarity-b.rarity) > 1e-9 {
			return cmp.Compare(b.rarity, a.rarity)
		}
		return strings.Compare(a.hit.ID, b.hit.ID)
	})
	var out []TitleHit
	for _, s := range found[:min(limit, len(found))] {
		out = append(out, s.hit)
	}
	return out, nil
}
