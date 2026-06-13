// Package ask assembles the deterministic half of `cerne ask`: it retrieves
// state-tagged context (compiled wiki + the FTS index) and renders the prompt.
// The answer itself comes from the host agent (delegation) or a Provider.
package ask

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/store"
)

// Snippet is one retrieved piece of context. State travels with every snippet
// so the answer can never present a superseded source as current.
type Snippet struct {
	Source string // entity ID, or "wiki:<rel>" for a compiled page
	Kind   string // feature | decision | reference | wiki
	State  string // lifecycle state; "current" for wiki pages
	Text   string
}

var terminalStates = map[string]bool{"superseded": true, "rejected": true, "obsolete": true}

// stopwords are common pt-BR + en function words dropped from a natural-language
// question before it reaches the FTS index, so the query keeps only salient
// terms instead of matching the whole sentence literally.
var stopwords = map[string]bool{
	// pt-BR
	"os": true, "as": true, "um": true, "uma": true, "de": true, "do": true,
	"da": true, "dos": true, "das": true, "em": true, "no": true, "na": true,
	"por": true, "que": true, "qual": true, "quais": true, "como": true,
	"quando": true, "onde": true, "porque": true, "para": true, "com": true,
	"sem": true, "foi": true, "está": true, "são": true, "ser": true,
	"sobre": true, "quanto": true, "será": true, "nos": true, "nas": true,
	"ao": true, "aos": true, "pelo": true, "pela": true,
	// en
	"the": true, "an": true, "of": true, "in": true, "on": true, "for": true,
	"how": true, "what": true, "why": true, "when": true, "where": true,
	"is": true, "are": true, "was": true, "were": true, "to": true,
	"and": true, "or": true, "with": true, "without": true, "about": true,
}

// extractKeywords turns a natural-language question into an FTS5 query: it
// tokenizes on non-letter/digit runes, lowercases, drops terms shorter than 3
// runes and stopwords, dedupes preserving order, and OR-joins the survivors.
// When nothing survives it returns the original query unchanged (index.Search
// already has a phrase fallback).
func extractKeywords(query string) string {
	tokens := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var kept []string
	seen := map[string]bool{}
	for _, tok := range tokens {
		t := strings.ToLower(tok)
		if len([]rune(t)) < 3 || stopwords[t] || seen[t] {
			continue
		}
		seen[t] = true
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return query
	}
	return strings.Join(kept, " OR ")
}

// Retrieve gathers context for the query: the FTS index hits (current-first;
// terminal entities only when history is true) plus the compiled wiki pages
// relevant to the hits — the project page (when present), the decisions page
// when any hit is a decision, the references page when any hit is a reference,
// and the module pages of the feature hits. Every snippet carries its state.
func Retrieve(repoDir, dbPath, query string, history bool, limit int) ([]Snippet, error) {
	q := extractKeywords(query)
	hits, err := index.Search(dbPath, q, limit)
	if err != nil {
		return nil, err
	}

	var entitySnips []Snippet
	modules := map[string]bool{}
	var sawDecision, sawReference bool
	for _, h := range hits {
		if !history && terminalStates[h.State] {
			continue
		}
		entitySnips = append(entitySnips, Snippet{Source: h.EntityID, Kind: h.Kind, State: h.State, Text: h.Snippet})
		switch h.Kind {
		case "feature":
			if f, err := store.LoadFeature(repoDir, h.EntityID); err == nil && f.Module != "" {
				modules[f.Module] = true
			}
		case "decision":
			sawDecision = true
		case "reference":
			sawReference = true
		}
	}

	// Wiki context first (high-level synthesis), then the entity hits. Order
	// among wiki pages: project (when present), decisions, references, modules.
	var snips []Snippet
	if s, ok := readWiki(repoDir, "project"); ok {
		snips = append(snips, s)
	}
	if sawDecision {
		if s, ok := readWiki(repoDir, "decisions"); ok {
			snips = append(snips, s)
		}
	}
	if sawReference {
		if s, ok := readWiki(repoDir, "references"); ok {
			snips = append(snips, s)
		}
	}
	mods := make([]string, 0, len(modules))
	for m := range modules {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	for _, m := range mods {
		if s, ok := readWiki(repoDir, "modules/"+m); ok {
			snips = append(snips, s)
		}
	}
	return append(snips, entitySnips...), nil
}

// ReadWikiPage loads a compiled page body (rel without extension, e.g.
// "project" or "modules/finance"), stripping its provenance frontmatter. ok is
// false when the page does not exist. Exported so the server can serve the same
// pages the ask retrieval reads.
func ReadWikiPage(repoDir, rel string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "wiki", filepath.FromSlash(rel)+".md"))
	if err != nil {
		return "", false
	}
	return stripFrontmatter(string(data)), true
}

// readWiki loads a compiled page (rel without extension, e.g. "project" or
// "modules/finance") as a state-tagged Snippet for the prompt.
func readWiki(repoDir, rel string) (Snippet, bool) {
	body, ok := ReadWikiPage(repoDir, rel)
	if !ok {
		return Snippet{}, false
	}
	return Snippet{Source: "wiki:" + rel, Kind: "wiki", State: "current", Text: body}, true
}

// stripFrontmatter removes a leading "---" block so the synthesis body alone
// reaches the prompt.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(strings.TrimLeft(s, " \t\n"), "---") {
		return s
	}
	lines := strings.Split(s, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			start = i
			break
		}
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return s
}

// BuildAskPrompt renders the question plus every state-tagged source, with the
// instruction to cite sources and respect each source's state.
func BuildAskPrompt(query string, snippets []Snippet) string {
	var b strings.Builder
	b.WriteString("Pergunta: " + query + "\n\n")
	b.WriteString("Responda em pt-BR usando apenas as fontes abaixo e cite-as (IDs/páginas). ")
	b.WriteString("Respeite o estado de cada fonte: não trate algo superseded/rejected/obsolete como vigente.\n\n")
	for _, s := range snippets {
		b.WriteString("## " + s.Source + " (" + s.Kind + ", " + s.State + ")\n")
		b.WriteString(strings.TrimSpace(s.Text) + "\n\n")
	}
	return b.String()
}
