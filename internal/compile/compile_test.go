package compile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
)

func seedBackend(t *testing.T, modules ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo", Modules: modules}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	return dir
}

func addFeature(t *testing.T, dir, id, module string, state domain.FeatureState, spec string) {
	t.Helper()
	if err := store.SaveFeature(dir, store.Feature{ID: id, Title: id, Module: module, State: state, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
	if spec != "" {
		p := filepath.Join(dir, "content", "features", id, "spec.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(spec), 0o644); err != nil {
			t.Fatalf("write spec: %v", err)
		}
	}
}

func pageByName(pages []Page, name string) (Page, bool) {
	for _, p := range pages {
		if p.Name == name {
			return p, true
		}
	}
	return Page{}, false
}

func sourceIDs(p Page) []string {
	var ids []string
	for _, s := range p.Sources {
		ids = append(ids, s.ID)
	}
	return ids
}

func pageRels(pages []Page) []string {
	rels := make([]string, 0, len(pages))
	for _, p := range pages {
		rels = append(rels, p.Rel)
	}
	return rels
}

// sourceContains reports whether any page lists the entity as a source.
func sourceContains(pages []Page, id string) bool {
	for _, p := range pages {
		for _, s := range p.Sources {
			if s.ID == id {
				return true
			}
		}
	}
	return false
}

// (a) Project WITH modules + active decisions + current references + a
// module-less feature: the page set is [modules..., decisions, references],
// there is NO project page, and the module-less feature appears in no page.
func TestPagesModuledSplitsDecisionsReferences(t *testing.T) {
	dir := seedBackend(t, "finance", "core")
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nspec.\n")
	addFeature(t, dir, "F-0002", "core", domain.StateCandidate, "# F-0002\n\nspec.\n")
	addFeature(t, dir, "F-0003", "finance", domain.StateSuperseded, "# F-0003\n\nold.\n") // not live
	addFeature(t, dir, "F-0099", "", domain.StateImplemented, "# F-0099\n\nsem módulo.\n")  // module-less
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0001", Title: "ADR um", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := store.SaveReference(dir, store.Reference{ID: "R-0001", Title: "Ref um", State: domain.ReferenceCurrent, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}

	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if got, want := pageRels(pages), []string{"modules/finance", "modules/core", "decisions", "references"}; !equalStrings(got, want) {
		t.Fatalf("page rels = %v, want %v", got, want)
	}
	if _, ok := pageByName(pages, "project"); ok {
		t.Fatalf("moduled project must not emit a project page: %v", pageRels(pages))
	}
	fin, _ := pageByName(pages, "finance")
	if len(fin.Sources) != 1 || fin.Sources[0].ID != "F-0001" {
		t.Fatalf("finance page = %+v", fin)
	}
	dec, ok := pageByName(pages, "decisions")
	if !ok || len(sourceIDs(dec)) != 1 || sourceIDs(dec)[0] != "D-0001" {
		t.Fatalf("decisions page = %+v, want [D-0001]", dec)
	}
	ref, ok := pageByName(pages, "references")
	if !ok || len(sourceIDs(ref)) != 1 || sourceIDs(ref)[0] != "R-0001" {
		t.Fatalf("references page = %+v, want [R-0001]", ref)
	}
	if sourceContains(pages, "F-0099") {
		t.Fatalf("module-less F-0099 must appear in no wiki page: %v", pageRels(pages))
	}
}

// (c) A moduled project with zero active decisions yields no decisions page.
func TestPagesModuledNoActiveDecisionsNoDecisionsPage(t *testing.T) {
	dir := seedBackend(t, "finance")
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nspec.\n")
	if err := store.SaveReference(dir, store.Reference{ID: "R-0001", Title: "Ref um", State: domain.ReferenceCurrent, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}
	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if _, ok := pageByName(pages, "decisions"); ok {
		t.Fatalf("no active decisions, but a decisions page was emitted: %v", pageRels(pages))
	}
	if got, want := pageRels(pages), []string{"modules/finance", "references"}; !equalStrings(got, want) {
		t.Fatalf("page rels = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPagesIncludesImplemented(t *testing.T) {
	dir := seedBackend(t, "finance")
	addFeature(t, dir, "F-0001", "finance", domain.StateImplemented, "# F-0001\n\nEntregue.\n")

	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	fin, ok := pageByName(pages, "finance")
	if !ok || len(fin.Sources) != 1 || fin.Sources[0].ID != "F-0001" {
		t.Fatalf("finance page = %+v, want implemented F-0001 as source", fin)
	}
}

// (b) Project with NO modules: page rels are [decisions, references, project]
// (decisions/references only when present), and the project page holds every
// live/implemented feature (the small-project fallback).
func TestPagesNoModules(t *testing.T) {
	dir := seedBackend(t)
	addFeature(t, dir, "F-0001", "", domain.StateCandidate, "# F-0001\n\nspec.\n")
	addFeature(t, dir, "F-0002", "", domain.StateImplemented, "# F-0002\n\nentregue.\n")
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0001", Title: "ADR", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := store.SaveReference(dir, store.Reference{ID: "R-0001", Title: "Ref", State: domain.ReferenceCurrent, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}
	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if got, want := pageRels(pages), []string{"decisions", "references", "project"}; !equalStrings(got, want) {
		t.Fatalf("page rels = %v, want %v", got, want)
	}
	dec, _ := pageByName(pages, "decisions")
	if ids := sourceIDs(dec); len(ids) != 1 || ids[0] != "D-0001" {
		t.Fatalf("decisions sources = %v, want [D-0001]", ids)
	}
	proj, ok := pageByName(pages, "project")
	if !ok {
		t.Fatalf("module-less project must emit a project page: %v", pageRels(pages))
	}
	if ids := sourceIDs(proj); len(ids) != 2 || ids[0] != "F-0001" || ids[1] != "F-0002" {
		t.Fatalf("project sources = %v, want [F-0001 F-0002] (all live/implemented features)", ids)
	}
}

// A project with no modules and no decisions/references emits only the project
// page (both cross-cutting pages omitted when empty).
func TestPagesNoModulesOnlyProjectWhenNoCrossCutting(t *testing.T) {
	dir := seedBackend(t)
	addFeature(t, dir, "F-0001", "", domain.StateCandidate, "# F-0001\n\nspec.\n")
	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if got, want := pageRels(pages), []string{"project"}; !equalStrings(got, want) {
		t.Fatalf("page rels = %v, want %v", got, want)
	}
}

func TestModulelessSkipped(t *testing.T) {
	// Moduled project: live features without a declared module are counted;
	// superseded ones (not wiki sources) and moduled ones are not.
	dir := seedBackend(t, "finance")
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n")        // moduled
	addFeature(t, dir, "F-0002", "", domain.StateImplemented, "# F-0002\n")            // module-less, counted
	addFeature(t, dir, "F-0003", "", domain.StateCandidate, "# F-0003\n")              // module-less, counted
	addFeature(t, dir, "F-0004", "", domain.StateSuperseded, "# F-0004\n")             // not a wiki source
	addFeature(t, dir, "F-0005", "ghost", domain.StateApproved, "# F-0005\n")          // module not declared, counted
	n, err := ModulelessSkipped(dir)
	if err != nil {
		t.Fatalf("ModulelessSkipped: %v", err)
	}
	if n != 3 {
		t.Fatalf("ModulelessSkipped (moduled) = %d, want 3", n)
	}
}

func TestModulelessSkippedNoModulesIsZero(t *testing.T) {
	// No declared modules: nothing is "skipped" — every feature is synthesized
	// on the project page — so the count is 0.
	dir := seedBackend(t)
	addFeature(t, dir, "F-0001", "", domain.StateApproved, "# F-0001\n")
	addFeature(t, dir, "F-0002", "", domain.StateImplemented, "# F-0002\n")
	n, err := ModulelessSkipped(dir)
	if err != nil {
		t.Fatalf("ModulelessSkipped: %v", err)
	}
	if n != 0 {
		t.Fatalf("ModulelessSkipped (no modules) = %d, want 0", n)
	}
}

// writeCompiled simulates a previously compiled page whose provenance matches
// the page's current input hash.
func writeCompiled(t *testing.T, dir string, p Page) {
	t.Helper()
	prov := wiki.Provenance{Page: p.Name, Sources: sourceIDs(p), CompiledFrom: "sha", InputHash: p.InputHash()}
	full := filepath.Join(dir, "content", "wiki", filepath.FromSlash(p.Rel)+".md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(wiki.RenderProvenance(prov)+"\n# "+p.Name+"\n\nsynthesis.\n"), 0o644); err != nil {
		t.Fatalf("write compiled: %v", err)
	}
}

func TestDirtyTracking(t *testing.T) {
	dir := seedBackend(t, "finance", "core")
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nspec.\n")
	addFeature(t, dir, "F-0002", "core", domain.StateCandidate, "# F-0002\n\nspec.\n")

	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	// Nothing compiled yet -> every page dirty.
	d, err := Dirty(dir, pages, false)
	if err != nil {
		t.Fatalf("Dirty: %v", err)
	}
	if len(d) != len(pages) {
		t.Fatalf("initial dirty = %d, want all %d", len(d), len(pages))
	}

	// Compile finance -> only finance becomes clean.
	fin, _ := pageByName(pages, "finance")
	writeCompiled(t, dir, fin)
	d, _ = Dirty(dir, pages, false)
	if _, dirty := pageByName(d, "finance"); dirty {
		t.Fatalf("finance still dirty after compile: %+v", d)
	}

	// Editing the finance feature makes finance dirty again.
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nEDITED.\n")
	pages, _ = Pages(dir)
	d, _ = Dirty(dir, pages, false)
	if _, dirty := pageByName(d, "finance"); !dirty {
		t.Fatalf("finance not dirty after content edit")
	}

	// Force marks everything dirty.
	d, _ = Dirty(dir, pages, true)
	if len(d) != len(pages) {
		t.Fatalf("force dirty = %d, want %d", len(d), len(pages))
	}
}

func TestBuildPagePromptResolvesSuperseded(t *testing.T) {
	dir := seedBackend(t, "finance")
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0003", Title: "ADR velho", State: domain.DecisionSuperseded, SupersededBy: "D-0002", CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision D-0003: %v", err)
	}
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0002", Title: "ADR novo", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision D-0002: %v", err)
	}
	// A live feature whose spec cites the superseded D-0003.
	addFeature(t, dir, "F-0001", "finance", domain.StateImplemented, "# Boletos\n\nDecisão base: D-0003.\n")

	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	p, ok := pageByName(pages, "finance")
	if !ok {
		t.Fatalf("no finance page in %v", pageRels(pages))
	}
	prompt := BuildPagePrompt(p)
	if !strings.Contains(prompt, "D-0002 (antes D-0003)") {
		t.Errorf("prompt should resolve the superseded id to its successor, got:\n%s", prompt)
	}
	if strings.Contains(strings.ReplaceAll(prompt, "(antes D-0003)", ""), "D-0003") {
		t.Errorf("prompt still presents D-0003 as a current reference:\n%s", prompt)
	}
	// The hash-feeding Content stays ORIGINAL (the rewrite is prompt-only), so
	// dirty-tracking does not re-fire on already-compiled pages.
	if len(p.Sources) != 1 || !strings.Contains(p.Sources[0].Content, "D-0003") || strings.Contains(p.Sources[0].Content, "antes") {
		t.Errorf("source Content must keep the original unresolved text, got: %q", p.Sources[0].Content)
	}
}

func TestPageSourceIncludesErrata(t *testing.T) {
	dir := seedBackend(t, "finance")
	addFeature(t, dir, "F-0001", "finance", domain.StateImplemented, "# Boletos\n\nSpec.\n")
	if err := store.AppendErrata(dir, "F-0001", "Correção: usar D-0002.", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	p, ok := pageByName(pages, "finance")
	if !ok {
		t.Fatalf("no finance page in %v", pageRels(pages))
	}
	if !strings.Contains(p.Sources[0].Content, "usar D-0002") {
		t.Errorf("source Content missing the errata text:\n%s", p.Sources[0].Content)
	}
	if !strings.Contains(BuildPagePrompt(p), "usar D-0002") {
		t.Errorf("prompt missing the errata text:\n%s", BuildPagePrompt(p))
	}
}

func TestErrataChangesInputHash(t *testing.T) {
	dir := seedBackend(t, "finance")
	addFeature(t, dir, "F-0001", "finance", domain.StateImplemented, "# Boletos\n\nSpec.\n")
	pages, _ := Pages(dir)
	p, _ := pageByName(pages, "finance")
	before := p.InputHash()

	if err := store.AppendErrata(dir, "F-0001", "nova errata", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	pages2, _ := Pages(dir)
	p2, _ := pageByName(pages2, "finance")
	if p2.InputHash() == before {
		t.Errorf("errata must change InputHash so the page re-synthesizes")
	}
}

func TestBuildPagePrompt(t *testing.T) {
	p := Page{Name: "finance", Rel: "modules/finance", Sources: []EntitySource{
		{ID: "F-0001", Kind: "feature", State: "approved", Title: "Boletos", Content: "# Boletos\n\nGera boletos.\n"},
	}}
	prompt := BuildPagePrompt(p)
	for _, want := range []string{"finance", "F-0001", "approved", "Boletos", "Gera boletos"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestValidatePageBody is F-0287 criterion 3: the defense against the failure
// mode that shipped. The agent wrote the page to a scratchpad file and answered
// with a report about it; cerne stamped the report with provenance, committed
// it, and the input-hash then called the page clean — the wiki was wrong and
// nothing noticed. A response that does not look like a page must not be
// accepted, whichever path produced it.
func TestValidatePageBody(t *testing.T) {
	// Verbatim from the F-0287 spec: what landed in content/wiki/project.md.
	report := "Página escrita.\n\n**Arquivo:** `.../scratchpad/project.md`\n\n" +
		"**Ações:**\n- Estados verificados via `cerne brief`\n- Página sintetizada\n"
	// Shaped like the real backend pages (title first, then prose).
	page := "# project\n\n" + strings.Repeat("Visão geral do estado corrente do projeto. ", 12)

	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"agent report instead of the page", report, true},
		{"empty", "", true},
		{"whitespace only", "\n\n   \n", true},
		{"prose before any title", "Segue a página do módulo.\n\n" + page, true},
		{"title but no page under it", "# project\n\nPágina escrita em /tmp/project.md.\n", true},
		{"a real page", page, false},
		{"a real page the agent wrapped in frontmatter", "---\nrun_id: abc\n---\n\n" + page, false},
		{"a real page under a deeper heading", "## Decisões\n\n" + page, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePageBody([]byte(tc.body))
			if tc.wantErr {
				if !errors.Is(err, ErrNotAPage) {
					t.Fatalf("ValidatePageBody = %v, want errors.Is ErrNotAPage", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePageBody = %v, want nil", err)
			}
		})
	}
}

// TestBuildPagePromptDemandsPageOnStdout is F-0287 criterion 1: the synthesis
// runs inside a Claude Code session, so the prompt must state the output
// contract the ingest side then enforces — the page itself on stdout, starting
// with a title, no files written and no report about what the agent did.
func TestBuildPagePromptDemandsPageOnStdout(t *testing.T) {
	p := Page{Name: "finance", Rel: "modules/finance", Sources: []EntitySource{
		{ID: "F-0001", Kind: "feature", State: "approved", Title: "Boletos", Content: "# Boletos\n"},
	}}
	prompt := BuildPagePrompt(p)
	for _, want := range []string{"saída padrão", "# ", "Não escreva arquivos", "não relate"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing the output contract %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPagePromptBoundsLongSource(t *testing.T) {
	short := "# Curto\n\nConteúdo pequeno.\n"
	long := "# Longo\n\n" + strings.Repeat("palavra ", 500) // well over the cap
	p := Page{Name: "finance", Rel: "modules/finance", Sources: []EntitySource{
		{ID: "F-0001", Kind: "feature", State: "implemented", Title: "Longo", Content: long},
		{ID: "F-0002", Kind: "feature", State: "approved", Title: "Curto", Content: short},
	}}
	prompt := BuildPagePrompt(p)

	if !contains(prompt, "…(conteúdo truncado)") {
		t.Fatalf("long source not truncated:\n%s", prompt)
	}
	// The whole long content (4000 chars) must not be embedded verbatim.
	if contains(prompt, long) {
		t.Fatalf("long source embedded in full despite the cap")
	}
	// The short source must appear whole and without a marker for it.
	if !contains(prompt, "Conteúdo pequeno.") {
		t.Fatalf("short source not emitted whole:\n%s", prompt)
	}

	// InputHash must hash FULL content, not the bounded excerpt: it must equal
	// the digest computed directly from each source's full content.
	want := wiki.InputHash([]wiki.Source{
		{ID: "F-0001", State: "implemented", ContentHash: contentHash(long)},
		{ID: "F-0002", State: "approved", ContentHash: contentHash(short)},
	})
	if got := p.InputHash(); got != want {
		t.Fatalf("InputHash = %s, want full-content hash %s (excerpting must not affect it)", got, want)
	}
}

func TestExcerptShortIsWhole(t *testing.T) {
	s := "linha curta sem corte"
	if got := excerpt(s); got != s {
		t.Fatalf("excerpt(short) = %q, want unchanged", got)
	}
	long := strings.Repeat("x", maxSourceExcerptChars+50)
	got := excerpt(long)
	if !strings.HasSuffix(got, "\n…(conteúdo truncado)") {
		t.Fatalf("excerpt(long) missing marker: %q", got)
	}
	body := strings.TrimSuffix(got, "\n…(conteúdo truncado)")
	if len([]rune(body)) > maxSourceExcerptChars {
		t.Fatalf("excerpt body = %d runes, want <= %d", len([]rune(body)), maxSourceExcerptChars)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestStale pins the freshness signal behind F-0316's banner (criteria 6 and
// 8): exactly the pages `cerne compile` would recompile — missing or with an
// old input hash — and never a page with no sources.
func TestStale(t *testing.T) {
	dir := seedBackend(t, "core", "empty")
	addFeature(t, dir, "F-0001", "core", domain.StateApproved, "# F-0001\n\nspec.\n")
	stale := func() []string {
		t.Helper()
		pages, err := Stale(dir)
		if err != nil {
			t.Fatalf("Stale: %v", err)
		}
		return pageRels(pages)
	}

	if got := stale(); !equalStrings(got, []string{"modules/core"}) {
		t.Fatalf("never compiled: Stale = %v, want [modules/core] (the sourceless page never)", got)
	}
	pages, err := Pages(dir)
	if err != nil {
		t.Fatal(err)
	}
	core, _ := pageByName(pages, "core")
	writeCompiled(t, dir, core)
	if got := stale(); len(got) != 0 {
		t.Fatalf("compiled with the current hash: Stale = %v, want none", got)
	}
	addFeature(t, dir, "F-0001", "core", domain.StateApproved, "# F-0001\n\nEDITED.\n")
	if got := stale(); !equalStrings(got, []string{"modules/core"}) {
		t.Fatalf("source changed after the compile: Stale = %v, want [modules/core]", got)
	}
}

// TestRunning pins the busy probe the on-demand recompile asks before running
// (F-0316): a live holder's lock is running; no lock, or one its holder has
// not touched within LockStaleAfter (a new compile would steal it), is not.
func TestRunning(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo")
	if Running(repoDir) {
		t.Fatal("no lock: Running = true")
	}
	if err := os.WriteFile(LockPath(repoDir), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Running(repoDir) {
		t.Fatal("fresh lock: Running = false")
	}
	old := time.Now().Add(-LockStaleAfter - time.Minute)
	if err := os.Chtimes(LockPath(repoDir), old, old); err != nil {
		t.Fatal(err)
	}
	if Running(repoDir) {
		t.Fatal("stale lock: Running = true")
	}
}
