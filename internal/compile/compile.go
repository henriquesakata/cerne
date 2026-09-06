// Package compile builds the deterministic half of `cerne compile`: the set
// of wiki pages a project should have, which of them are stale (input-hash),
// and the prompt that synthesizes each. The non-deterministic half — the
// synthesis text — is produced by the host agent (delegation) or a Provider.
package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// EntitySource is one live entity feeding a wiki page, with the content the
// prompt needs and the state every consumer must respect.
type EntitySource struct {
	ID      string
	Kind    string // feature | decision | reference
	State   string
	Title   string
	Content string // ORIGINAL source content — what InputHash hashes (dirty-tracking)
	// promptContent is Content with references to superseded entities resolved to
	// their terminal successor ("D-0002 (antes D-0003)"); it feeds the prompt only,
	// so the rewrite never changes InputHash. Empty means "use Content unchanged".
	promptContent string
}

// Page is one synthesis page: a module's live features, the active-decisions
// page, the current-references page, or — only when the project declares no
// modules — the cross-cutting "project" page holding every live entity.
type Page struct {
	Name    string // module name, "decisions", "references", or "project"
	Rel     string // path under content/wiki without extension ("modules/x" | "decisions" | "references" | "project")
	Sources []EntitySource
}

// wikiSourceStates are the feature states that feed the wiki. Unlike lint /
// materialize (which target the current-work view and exclude implemented),
// the wiki must synthesize the DELIVERED system, so implemented is included.
// This set is local to package compile.
var wikiSourceStates = map[domain.FeatureState]bool{
	domain.StateCandidate:     true,
	domain.StateApproved:      true,
	domain.StateInDevelopment: true,
	domain.StateImplemented:   true,
}

// Pages computes the deterministic page set for the backend at repoDir:
//   - one page per declared module (its live features), in proj.Modules order;
//   - a "decisions" page (active decisions), only when at least one exists;
//   - a "references" page (current references), only when at least one exists;
//   - a "project" page, ONLY when the project declares no modules, holding
//     every live entity (the small-project fallback).
//
// When the project HAS modules, live features without a declared module are
// synthesized in NO page — they are covered by CONTEXT.md / search instead
// (see ModulelessSkipped). Page order is deterministic: modules, decisions,
// references, project.
func Pages(repoDir string) ([]Page, error) {
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		return nil, err
	}
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

	var pages []Page
	for _, m := range proj.Modules {
		var srcs []EntitySource
		for _, f := range features {
			if wikiSourceStates[f.State] && f.Module == m {
				srcs = append(srcs, featureSource(repoDir, f))
			}
		}
		pages = append(pages, Page{Name: m, Rel: "modules/" + m, Sources: srcs})
	}

	// decisions page: active decisions, only when at least one exists.
	var dsrcs []EntitySource
	for _, d := range decisions {
		if d.State == domain.DecisionActive {
			dsrcs = append(dsrcs, decisionSource(repoDir, d))
		}
	}
	if len(dsrcs) > 0 {
		pages = append(pages, Page{Name: "decisions", Rel: "decisions", Sources: dsrcs})
	}

	// references page: current references, only when at least one exists.
	var rsrcs []EntitySource
	for _, r := range references {
		if r.State == domain.ReferenceCurrent {
			rsrcs = append(rsrcs, referenceSource(repoDir, r))
		}
	}
	if len(rsrcs) > 0 {
		pages = append(pages, Page{Name: "references", Rel: "references", Sources: rsrcs})
	}

	// project page: the small-project fallback, only when no modules are
	// declared. It holds every live/implemented feature. With modules, the
	// module-less features are deliberately omitted from the wiki.
	if len(proj.Modules) == 0 {
		var proje []EntitySource
		for _, f := range features {
			if wikiSourceStates[f.State] {
				proje = append(proje, featureSource(repoDir, f))
			}
		}
		pages = append(pages, Page{Name: "project", Rel: "project", Sources: proje})
	}
	resolveSupersededInPages(repoDir, pages)
	return pages, nil
}

// resolveSupersededInPages fills each source's promptContent with its Content
// rewritten so a reference to a superseded entity resolves to the terminal
// successor. Run after assembly so the resolver sees every entity; Content (and
// thus InputHash) is left untouched — the resolution feeds the prompt only.
func resolveSupersededInPages(repoDir string, pages []Page) {
	resolve := func(id string) (string, bool) {
		succ, ok, err := app.TerminalSuccessor(repoDir, id)
		if err != nil || !ok {
			return "", false
		}
		return succ, true
	}
	for pi := range pages {
		for si := range pages[pi].Sources {
			pages[pi].Sources[si].promptContent = rewriteSupersededRefs(pages[pi].Sources[si].Content, resolve)
		}
	}
}

// ModulelessSkipped reports how many live (wiki-source) features the wiki
// deliberately omits because they have no declared module. It is 0 when the
// project declares no modules — there every live feature is synthesized on the
// project page, so nothing is skipped. A feature whose module is not among the
// declared modules counts as skipped (it lands in no module page). The compile
// command surfaces this count so the omission is never silent.
func ModulelessSkipped(repoDir string) (int, error) {
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		return 0, err
	}
	if len(proj.Modules) == 0 {
		return 0, nil
	}
	moduleSet := make(map[string]bool, len(proj.Modules))
	for _, m := range proj.Modules {
		moduleSet[m] = true
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range features {
		if wikiSourceStates[f.State] && !moduleSet[f.Module] {
			n++
		}
	}
	return n, nil
}

// InputHash is the digest of the page's sources for dirty-tracking.
func (p Page) InputHash() string {
	ws := make([]wiki.Source, 0, len(p.Sources))
	for _, s := range p.Sources {
		ws = append(ws, wiki.Source{ID: s.ID, State: s.State, ContentHash: contentHash(s.Content)})
	}
	return wiki.InputHash(ws)
}

// Dirty returns the pages that need recompilation: those whose compiled page
// is missing or whose recorded input_hash differs from the current one. With
// force, every page is returned.
func Dirty(repoDir string, pages []Page, force bool) ([]Page, error) {
	var dirty []Page
	for _, p := range pages {
		if force {
			dirty = append(dirty, p)
			continue
		}
		data, err := os.ReadFile(pagePath(repoDir, p))
		if errors.Is(err, fs.ErrNotExist) {
			dirty = append(dirty, p)
			continue
		}
		if err != nil {
			return nil, err
		}
		if wiki.ParseProvenance(data).InputHash != p.InputHash() {
			dirty = append(dirty, p)
		}
	}
	return dirty, nil
}

// maxSourceExcerptChars bounds each source's content in the synthesis prompt so
// a page aggregating many entities (e.g. the cross-cutting project page) stays
// within one LLM call. It bounds only the PROMPT text — InputHash still hashes
// each source's full content (see InputHash), so deep edits keep marking dirty.
const maxSourceExcerptChars = 800

// excerpt returns content unchanged when within the cap, otherwise the first
// maxSourceExcerptChars runes (cut on a rune boundary, preferring the last
// newline within the cap) followed by a truncation marker.
func excerpt(content string) string {
	r := []rune(content)
	if len(r) <= maxSourceExcerptChars {
		return content
	}
	head := string(r[:maxSourceExcerptChars])
	if nl := strings.LastIndexByte(head, '\n'); nl > 0 {
		head = head[:nl]
	}
	return head + "\n…(conteúdo truncado)"
}

// BuildPagePrompt renders the deterministic synthesis prompt for a page: the
// instruction plus every source with its state and a bounded excerpt of its
// content. The text the LLM returns is the page body; cerne stamps the
// provenance frontmatter on ingest. The instruction states the output contract
// ValidatePageBody enforces on the way back in (F-0287).
func BuildPagePrompt(p Page) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Sintetize a página de wiki \"%s\" do projeto a partir das fontes vigentes abaixo.\n", p.Name)
	b.WriteString("Escreva markdown em pt-BR: visão geral + como as partes se relacionam hoje. ")
	b.WriteString("Respeite o estado de cada fonte e não invente o que não está nas fontes.\n")
	// F-0287: this prompt is answered by a Claude Code session, not by a bare
	// completion endpoint. Say what the answer IS, or it may answer with a
	// report about having produced it elsewhere.
	b.WriteString("Devolva SOMENTE o markdown da página na saída padrão, começando por um título \"# \". ")
	b.WriteString("Não escreva arquivos, não use ferramentas e não relate o que você fez.\n\n")
	for _, s := range p.Sources {
		content := s.Content
		if s.promptContent != "" {
			content = s.promptContent
		}
		fmt.Fprintf(&b, "## %s — %s (%s, %s)\n\n%s\n\n", s.ID, s.Title, s.Kind, s.State, excerpt(strings.TrimSpace(content)))
	}
	return b.String()
}

func featureSource(repoDir string, f store.Feature) EntitySource {
	return EntitySource{ID: f.ID, Kind: "feature", State: string(f.State), Title: f.Title,
		Content: withErrata(repoDir, f.ID, readFeatureContent(repoDir, f.ID))}
}

func decisionSource(repoDir string, d store.Decision) EntitySource {
	return EntitySource{ID: d.ID, Kind: "decision", State: string(d.State), Title: d.Title,
		Content: withErrata(repoDir, d.ID, readFile(filepath.Join(repoDir, "content", "decisions", d.ID+".md")))}
}

func referenceSource(repoDir string, r store.Reference) EntitySource {
	return EntitySource{ID: r.ID, Kind: "reference", State: string(r.State), Title: r.Title,
		Content: withErrata(repoDir, r.ID, readFile(filepath.Join(repoDir, "content", "references", r.ID+".md")))}
}

// withErrata appends the entity's errata (if any) to its content, so a correction
// on a concluded, immutable entity reaches the synthesis prompt AND the input
// hash (so the page re-synthesizes). The errata's own frontmatter is dropped —
// only the dated entries feed the page.
func withErrata(repoDir, id, content string) string {
	er, err := store.LoadErrata(repoDir, id)
	if err != nil {
		return content
	}
	er = strings.TrimSpace(stripFrontmatter(er))
	if er == "" {
		return content
	}
	return content + "\n\n" + er
}

// readFeatureContent concatenates the feature's existing lifecycle documents
// in spec→plan→result order.
func readFeatureContent(repoDir, id string) string {
	var b strings.Builder
	for _, name := range []string{"spec.md", "plan.md", "result.md"} {
		if c := readFile(filepath.Join(repoDir, "content", "features", id, name)); c != "" {
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// pagePath is the backend path of a page's compiled markdown file.
func pagePath(repoDir string, p Page) string {
	return filepath.Join(repoDir, "content", "wiki", filepath.FromSlash(p.Rel)+".md")
}
