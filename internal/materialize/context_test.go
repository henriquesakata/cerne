package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// E-0005 verification 1 (criteria 1 and 2): CONTEXT.md lists only what is
// live; the done features become a count per state with the command that lists
// them; an epic shows only with a live child; notes and references come as the
// components of the active notes with their counts and the group search, not
// one line per entity — while their files are still materialized.
func TestContextListsOnlyWhatIsLive(t *testing.T) {
	backend := seedBackend(t, "core")
	save := func(f store.Feature) {
		t.Helper()
		f.CreatedAt, f.UpdatedAt = seedTime, seedTime
		if err := store.SaveFeature(backend, f); err != nil {
			t.Fatal(err)
		}
	}
	save(store.Feature{ID: "F-0001", Title: "Candidata viva", State: domain.StateCandidate})
	save(store.Feature{ID: "F-0002", Title: "Aprovada viva", State: domain.StateApproved})
	save(store.Feature{ID: "F-0003", Title: "Em curso", State: domain.StateInDevelopment})
	save(store.Feature{ID: "F-0004", Title: "Esperando aceite", State: domain.StateImplemented})
	save(store.Feature{ID: "F-0005", Title: "Aceita antiga", State: domain.StateDone})
	save(store.Feature{ID: "F-0006", Title: "Aceita velha", State: domain.StateDone})
	save(store.Feature{ID: "F-0007", Title: "Recusada", State: domain.StateRejected})
	save(store.Feature{ID: "E-0001", Title: "Epico vivo", State: domain.StateInDevelopment})
	save(store.Feature{ID: "T-0001", Title: "Filha viva", Parent: "E-0001", State: domain.StateApproved})
	save(store.Feature{ID: "T-0002", Title: "Filha entregue", Parent: "E-0001", State: domain.StateDone})
	save(store.Feature{ID: "E-0002", Title: "Epico so de concluidas", State: domain.StateInDevelopment})
	save(store.Feature{ID: "T-0003", Title: "Filha concluida", Parent: "E-0002", State: domain.StateDone})
	for i, comps := range [][]string{{"gitea", "kpack"}, {"gitea"}, {"ci"}} {
		id := fmt.Sprintf("N-%04d", i+1)
		addNote(t, backend, id, "Nota "+id, domain.NoteActive, "---\ncerne_id: "+id+"\n---\n\nCorpo.\n")
		n, _ := store.LoadNote(backend, id)
		n.Components = comps
		if err := store.SaveNote(backend, n); err != nil {
			t.Fatal(err)
		}
	}
	addNote(t, backend, "N-0004", "Nota velha", domain.NoteSuperseded, "---\ncerne_id: N-0004\n---\n\nX.\n")
	addReference(t, backend, "R-0001", "Fonte viva", domain.ReferenceCurrent, "reference R-0001\n")

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatal(err)
	}
	ctx := readContext(t, docsDir)
	for _, live := range []string{"| F-0001 | candidate |", "| F-0002 | approved |", "| F-0003 | in_development |", "| F-0004 | implemented |", "| T-0001 | approved | E-0001 |"} {
		if !strings.Contains(ctx, live) {
			t.Errorf("live row %q missing:\n%s", live, ctx)
		}
	}
	for _, gone := range []string{"F-0005", "F-0006", "F-0007", "T-0002", "T-0003", "### E-0002", "N-0001", "R-0001", "Fonte viva"} {
		if strings.Contains(ctx, gone) {
			t.Errorf("CONTEXT.md lists %q, which is not live or is a per-entity line:\n%s", gone, ctx)
		}
	}
	for _, want := range []string{
		"Not listed: 4 done, 1 rejected — 'cerne feature list --state <state>' lists them.",
		"### E-0001 — Epico vivo (1/2 implemented)",
		"3 active notes, 1 current reference.",
		"Components of the active notes: ci (1), gitea (2), kpack (1).",
		"cerne note list --component a,b,c",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("CONTEXT.md missing %q:\n%s", want, ctx)
		}
	}
	for _, rel := range []string{"current/notes/N-0001-nota-n-0001.md", "current/references/R-0001-fonte-viva.md"} {
		if _, err := os.Stat(filepath.Join(docsDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s no longer materialized: %v", rel, err)
		}
	}
}

// seedLiveFeatures saves n features cycling through the live states, each
// with a title long enough that a few hundred overflow the map.
func seedLiveFeatures(t testing.TB, backend string, n int) {
	t.Helper()
	states := []domain.FeatureState{domain.StateCandidate, domain.StateApproved, domain.StateImplemented, domain.StateInDevelopment}
	for i := 1; i <= n; i++ {
		f := store.Feature{ID: fmt.Sprintf("F-%04d", i), Title: fmt.Sprintf("Feature viva numero %d com um titulo de tamanho realista para o mapa", i),
			State: states[i%len(states)], CreatedAt: seedTime, UpdatedAt: seedTime}
		if err := store.SaveFeature(backend, f); err != nil {
			t.Fatal(err)
		}
	}
}

// E-0005 verification 2 (criterion 3): 500 live features do not fit; the map
// stays within 16 KB, keeps them in priority order (in development, awaiting
// acceptance, approved, candidate) and says how many it left out and how to
// list them.
func TestContextFitsTheCeiling(t *testing.T) {
	backend := seedBackend(t)
	seedLiveFeatures(t, backend, 500)
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatal(err)
	}
	ctx := readContext(t, docsDir)
	if len(ctx) > contextMaxBytes {
		t.Fatalf("CONTEXT.md is %d bytes, over the %d ceiling", len(ctx), contextMaxBytes)
	}
	count := func(state string) int { return strings.Count(ctx, "| "+state+" |") }
	impl := count("implemented")
	if count("in_development") != 125 || impl == 0 || impl == 125 || count("approved") != 0 || count("candidate") != 0 {
		t.Fatalf("cut out of order: %d in_development (want 125), %d implemented (want some, not all), %d approved and %d candidate (want 0)\n%s",
			count("in_development"), impl, count("approved"), count("candidate"), ctx)
	}
	want := fmt.Sprintf("%d live features left out to keep this file within 16 KB (%d implemented, 125 approved, 125 candidate) — 'cerne feature list --state <state>' lists them.", 375-impl, 125-impl)
	if !strings.Contains(ctx, want) {
		t.Fatalf("missing the left-out line %q:\n%s", want, ctx)
	}
	// Newest first within a state: F-0498 is the last implemented, F-0002 the first.
	if !strings.Contains(ctx, "| F-0498 | implemented |") || strings.Contains(ctx, "| F-0002 |") {
		t.Fatalf("the cut does not keep the newest implemented first:\n%s", ctx)
	}
	if strings.Contains(ctx, "_Cut at 16 KB") {
		t.Fatalf("hard cut although the features alone could be trimmed:\n%s", ctx)
	}
}

// E-0005 criterion 3, the other sections: 150 active decisions do not push
// out the in-development rows nor the tag list and the group search; the
// decisions are trimmed and counted, and nothing is cut blind.
func TestContextTrimsEverySection(t *testing.T) {
	backend := seedBackend(t)
	for i := 1; i <= 5; i++ {
		f := store.Feature{ID: fmt.Sprintf("F-%04d", i), Title: fmt.Sprintf("Em curso %d", i), State: domain.StateInDevelopment, CreatedAt: seedTime, UpdatedAt: seedTime}
		if err := store.SaveFeature(backend, f); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 150; i++ {
		id := fmt.Sprintf("D-%04d", i)
		addDecision(t, backend, id, fmt.Sprintf("Decisao ativa numero %d com um titulo do tamanho de uma ADR real", i), domain.DecisionActive, "decision "+id+"\n")
	}
	addNote(t, backend, "N-0001", "Nota", domain.NoteActive, "---\ncerne_id: N-0001\n---\n\nCorpo.\n")
	n, _ := store.LoadNote(backend, "N-0001")
	n.Components = []string{"gitea"}
	if err := store.SaveNote(backend, n); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatal(err)
	}
	ctx := readContext(t, docsDir)
	if len(ctx) > contextMaxBytes || strings.Contains(ctx, "_Cut at 16 KB") {
		t.Fatalf("CONTEXT.md is %d bytes or cut blind:\n%s", len(ctx), ctx)
	}
	if got := strings.Count(ctx, "| in_development |"); got != 5 {
		t.Fatalf("%d in-development rows, want all 5:\n%s", got, ctx)
	}
	kept := strings.Count(ctx, "- [D-")
	for _, want := range []string{
		fmt.Sprintf("- %d more left out to keep this file within 16 KB: cerne decision list", 150-kept),
		"Components of the active notes: gitea (1).",
		"cerne note list --component a,b,c",
	} {
		if kept == 0 || kept == 150 || !strings.Contains(ctx, want) {
			t.Fatalf("CONTEXT.md missing %q (%d decisions kept):\n%s", want, kept, ctx)
		}
	}
}

// seedScaleBackend is a synthetic backend of n entities, mostly done features
// as in a mature project, with live features carrying their documents (so
// their rows carry the links), decisions, runbooks, active works, notes and
// references beside them.
func seedScaleBackend(t testing.TB, n int) string {
	t.Helper()
	backend := seedBackend(t)
	decs, rbs, works := n/80, n/500, n/160
	notes, refs, live := n/8, n/8, n/20
	feats := n - notes - refs - decs - rbs - works
	seedLiveFeatures(t, backend, live)
	for i := 1; i <= live; i++ {
		dir := filepath.Join(backend, "content", "features", fmt.Sprintf("F-%04d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("spec\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= decs; i++ {
		id := fmt.Sprintf("D-%04d", i)
		addDecision(t, backend, id, fmt.Sprintf("Decisao ativa numero %d com um titulo do tamanho de uma ADR real", i), domain.DecisionActive, "decision "+id+"\n")
	}
	for i := 1; i <= rbs; i++ {
		addRunbook(t, backend, fmt.Sprintf("RB-%04d", i), fmt.Sprintf("Runbook ativo %d", i), domain.RunbookActive)
	}
	for i := 1; i <= works; i++ {
		addWork(t, backend, fmt.Sprintf("W-%04d", i), fmt.Sprintf("Trabalho em andamento numero %d", i), "agent:m-1", domain.WorkActive, time.Now().UTC())
	}
	for i := live + 1; i <= feats; i++ {
		f := store.Feature{ID: fmt.Sprintf("F-%04d", i), Title: fmt.Sprintf("Feature concluida %d", i), State: domain.StateDone,
			Components: []string{fmt.Sprintf("comp%d", i%150)}, CreatedAt: seedTime, UpdatedAt: seedTime}
		if err := store.SaveFeature(backend, f); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= notes; i++ {
		id := fmt.Sprintf("N-%04d", i)
		addNote(t, backend, id, "Nota "+id, domain.NoteActive, "---\ncerne_id: "+id+"\n---\n\nCorpo.\n")
		nt, _ := store.LoadNote(backend, id)
		nt.Components = []string{fmt.Sprintf("comp%d", i%150), fmt.Sprintf("area%d", i%40)}
		if err := store.SaveNote(backend, nt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= refs; i++ {
		id := fmt.Sprintf("R-%04d", i)
		addReference(t, backend, id, "Fonte "+id, domain.ReferenceCurrent, "reference "+id+"\n")
	}
	return backend
}

// E-0005 verification 12 (criterion 17): with 5,000 entities the map stays in
// 16 KB, keeps in-development rows and the tag list, and is never cut blind.
// The pull-time half is BenchmarkMaterialize5000, run before and after.
func TestContextAt5000Entities(t *testing.T) {
	backend := seedScaleBackend(t, 5000)
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatal(err)
	}
	ctx := readContext(t, docsDir)
	if len(ctx) > contextMaxBytes || strings.Contains(ctx, "_Cut at 16 KB") {
		t.Fatalf("CONTEXT.md is %d bytes with 5,000 entities (ceiling %d), or cut blind:\n%s", len(ctx), contextMaxBytes, ctx)
	}
	for _, want := range []string{"| in_development |", "[spec](current/features/", "Components of the active notes:", "cerne note list --component a,b,c", "left out to keep this file within 16 KB"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("CONTEXT.md at 5,000 entities missing %q:\n%s", want, ctx)
		}
	}
}

// BenchmarkMaterialize5000 times the whole projection (what 'cerne pull' does
// once the backend is fetched) over the 5,000-entity backend.
func BenchmarkMaterialize5000(b *testing.B) {
	backend := seedScaleBackend(b, 5000)
	docsDir := filepath.Join(b.TempDir(), ".docs")
	b.ResetTimer()
	for b.Loop() {
		if _, err := Materialize(backend, docsDir, "", false); err != nil {
			b.Fatal(err)
		}
	}
}
