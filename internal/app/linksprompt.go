package app

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// ErrNoLinkSource: an id asked for is not an entity with a content document.
var ErrNoLinkSource = errors.New("no document to read")

// LinkPromptMarker opens every link-suggestion prompt, so a test can tell it
// from any other prompt sent to the LLM.
const LinkPromptMarker = "[cerne:link-suggestions]"

// LinkAnswerTokens is the answer budget the estimate adds per document.
const LinkAnswerTokens = 300

// LinkSource is one entity with its content documents joined.
type LinkSource struct{ ID, Title, Text string }

// LinkTarget is one entity a suggestion may point at.
type LinkTarget struct{ ID, Title string }

// LoadLinkSources returns the entities whose documents are read — the ids
// given, or every entity with a content document — and the catalog of every
// entity (the possible targets).
func LoadLinkSources(repoDir string, ids []string) ([]LinkSource, []LinkTarget, error) {
	g, err := BuildGraph(repoDir)
	if err != nil {
		return nil, nil, err
	}
	docs, err := allContentDocs(repoDir)
	if err != nil {
		return nil, nil, err
	}
	title := map[string]string{}
	var catalog []LinkTarget
	for _, n := range g.Nodes {
		title[n.ID] = n.Title
		catalog = append(catalog, LinkTarget{n.ID, n.Title})
	}
	text := map[string]*strings.Builder{}
	var order []string
	for _, d := range docs {
		b, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(d.rel)))
		if err != nil {
			return nil, nil, err
		}
		if text[d.id] == nil {
			text[d.id] = &strings.Builder{}
			order = append(order, d.id)
		}
		fmt.Fprintf(text[d.id], "--- %s ---\n%s\n", path.Base(d.rel), b)
	}
	if len(ids) > 0 {
		for _, id := range ids {
			if text[id] == nil {
				return nil, nil, fmt.Errorf("%w: %s", ErrNoLinkSource, id)
			}
		}
		order = slices.Clone(ids)
	}
	sort.Strings(order)
	var out []LinkSource
	for _, id := range slices.Compact(order) {
		out = append(out, LinkSource{ID: id, Title: title[id], Text: text[id].String()})
	}
	return out, catalog, nil
}

// BuildLinkPrompt asks for the relations src mentions without a link. The
// catalog lists every other entity, the only ids the answer may use.
func BuildLinkPrompt(src LinkSource, catalog []LinkTarget) string {
	var b strings.Builder
	b.WriteString(LinkPromptMarker + "\n")
	b.WriteString("You find relations a document mentions in prose but does not link.\n\n")
	fmt.Fprintf(&b, "Source document: %s — %s\n\n", src.ID, src.Title)
	b.WriteString("Catalog of the entities it may point at (id — title):\n")
	for _, t := range catalog {
		if t.ID != src.ID {
			fmt.Fprintf(&b, "%s — %s\n", t.ID, t.Title)
		}
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Suggest a relation only when the document clearly refers to a catalog entity by name or description and does not already link it with [[ID]] or a markdown link.\n")
	b.WriteString("- \"to\" is an id from the catalog; never the source itself.\n")
	b.WriteString("- \"excerpt\" is copied from the document character for character, at most one sentence, and it is what mentions the target.\n")
	b.WriteString("- When in doubt, leave it out. No commentary.\n\n")
	fmt.Fprintf(&b, "Answer with only a JSON array, [] when there is nothing: [{\"from\":%q,\"to\":\"<catalog id>\",\"excerpt\":\"<text from the document>\"}]\n\n", src.ID)
	b.WriteString("Document:\n")
	b.WriteString(src.Text)
	return b.String()
}

// EstimateLinkTokens is a rough token estimate for sending prompts: half
// of their runes (measured ~2.14 runes/token on pt-BR catalogs: err high), plus LinkAnswerTokens of answer for each.
func EstimateLinkTokens(prompts []string) int {
	n := 0
	for _, p := range prompts {
		n += utf8.RuneCountInString(p)/2 + LinkAnswerTokens
	}
	return n
}
