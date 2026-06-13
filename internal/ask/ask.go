// Package ask assembles the deterministic half of `cerne ask`: it retrieves
// state-tagged context (compiled wiki + the FTS index) and renders the prompt.
// The answer itself comes from the host agent (delegation) or a Provider.
package ask

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// Retrieve gathers context for the query: the FTS index hits (current-first;
// terminal entities only when history is true) plus the compiled wiki pages
// relevant to the hits — the project page and the module pages of the feature
// hits. Every snippet carries its state.
func Retrieve(repoDir, dbPath, query string, history bool, limit int) ([]Snippet, error) {
	hits, err := index.Search(dbPath, query, limit)
	if err != nil {
		return nil, err
	}

	var entitySnips []Snippet
	modules := map[string]bool{}
	for _, h := range hits {
		if !history && terminalStates[h.State] {
			continue
		}
		entitySnips = append(entitySnips, Snippet{Source: h.EntityID, Kind: h.Kind, State: h.State, Text: h.Snippet})
		if h.Kind == "feature" {
			if f, err := store.LoadFeature(repoDir, h.EntityID); err == nil && f.Module != "" {
				modules[f.Module] = true
			}
		}
	}

	// Wiki context first (high-level synthesis), then the entity hits.
	var snips []Snippet
	if s, ok := readWiki(repoDir, "project"); ok {
		snips = append(snips, s)
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

// readWiki loads a compiled page (rel without extension, e.g. "project" or
// "modules/finance"), stripping its provenance frontmatter from the body.
func readWiki(repoDir, rel string) (Snippet, bool) {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "wiki", filepath.FromSlash(rel)+".md"))
	if err != nil {
		return Snippet{}, false
	}
	return Snippet{Source: "wiki:" + rel, Kind: "wiki", State: "current", Text: stripFrontmatter(string(data))}, true
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
