package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// newFeatureBackend sets up a backend clone with a bare origin and a docs.lock,
// as CreateFeature/SaveFeatureDoc commit, push and refresh the lock snapshot.
func newFeatureBackend(t *testing.T, modules ...string) (repoDir, projDir, origin string) {
	t.Helper()
	base := t.TempDir()
	origin = filepath.Join(base, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(base, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo", Modules: modules}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	projDir = filepath.Join(base, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("MkdirAll proj: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	return repoDir, projDir, origin
}

func TestCreateFeature(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)

	f, err := CreateFeature(repoDir, projDir, "  Minha feature  ", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if f.ID != "F-0001" || f.Title != "Minha feature" || f.State != domain.StateCandidate {
		t.Fatalf("feature = %+v, want F-0001 'Minha feature' candidate (title trimmed)", f)
	}

	got, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil || got.State != domain.StateCandidate {
		t.Fatalf("LoadFeature = %+v, %v; want persisted candidate", got, err)
	}

	// The spec stub must not count as real content (approve gate still blocks).
	ds, err := FeatureDocSet(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("FeatureDocSet: %v", err)
	}
	if ds.HasSpec {
		t.Fatalf("spec stub should not count as real content")
	}

	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "feature.created" || evs[0].EntityID != "F-0001" {
		t.Fatalf("events = %+v, want one feature.created for F-0001", evs)
	}

	// Committed + pushed; lock advanced to the new origin HEAD.
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	head, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if lock.Snapshot == "" || lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %q, want origin HEAD %q", lock.Snapshot, head)
	}
}

// TestCreateFeatureWithSpec pins the one-shot path (F-0120): a feature born
// with a real spec body, stamped with the cerne_id frontmatter, that already
// counts as real content (the approve gate opens without a later push).
func TestCreateFeatureWithSpec(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)

	body := "# Minha feature\n\n## Problema\n\nAgentes esquecem a spec.\n"
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Minha feature", "", "", nil, nil, body)
	if err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}

	data, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "spec.md"))
	if err != nil {
		t.Fatalf("ReadFile spec: %v", err)
	}
	want := "---\ncerne_id: " + f.ID + "\n---\n\n" + body
	if string(data) != want {
		t.Fatalf("spec.md = %q, want stamped body %q", data, want)
	}

	ds, err := FeatureDocSet(repoDir, f.ID)
	if err != nil {
		t.Fatalf("FeatureDocSet: %v", err)
	}
	if !ds.HasSpec {
		t.Fatalf("a spec born with a real body should count as real content")
	}
}

// TestCreateFeatureWithSpecEmptyBodyScaffoldsStub pins that the empty body
// degrades to the current stub behavior.
func TestCreateFeatureWithSpecEmptyBodyScaffoldsStub(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Sem corpo", "", "", nil, nil, "   ")
	if err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	ds, err := FeatureDocSet(repoDir, f.ID)
	if err != nil {
		t.Fatalf("FeatureDocSet: %v", err)
	}
	if ds.HasSpec {
		t.Fatalf("empty body must scaffold the stub (not real content)")
	}
}

func TestCreateFeatureEmptyTitle(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "   ", "", "", nil, nil); err == nil {
		t.Fatalf("want error for empty title")
	}
}

func TestCreateFeatureWithParent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child, err := CreateFeature(repoDir, projDir, "Filha", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}
	if child.Parent != epic.ID {
		t.Fatalf("child.Parent = %q, want %q", child.Parent, epic.ID)
	}
	got, err := store.LoadFeature(repoDir, child.ID)
	if err != nil || got.Parent != epic.ID {
		t.Fatalf("persisted parent = %q, %v; want %q", got.Parent, err, epic.ID)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "feature.created" || last.EntityID != child.ID || last.Payload["parent"] != epic.ID {
		t.Fatalf("event = %+v, want feature.created for child carrying parent", last)
	}
}

func TestCreateFeatureParentErrors(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "X", "", "F-9999", nil, nil); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("err = %v, want ErrParentNotFound", err)
	}
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	epic.State = domain.StateRejected // drive it terminal directly
	if err := store.SaveFeature(repoDir, epic); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := CreateFeature(repoDir, projDir, "Filha", "", epic.ID, nil, nil); !errors.Is(err, ErrParentTerminal) {
		t.Fatalf("err = %v, want ErrParentTerminal", err)
	}
}

func TestCreateFeatureModuleRules(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t, "core", "web")

	if _, err := CreateFeature(repoDir, projDir, "X", "", "", nil, nil); err == nil {
		t.Fatalf("want error: --module required with 2+ declared modules")
	}
	if _, err := CreateFeature(repoDir, projDir, "X", "nope", "", nil, nil); err == nil {
		t.Fatalf("want error: undeclared module")
	}
	f, err := CreateFeature(repoDir, projDir, "X", "core", "", nil, nil)
	if err != nil || f.Module != "core" {
		t.Fatalf("CreateFeature valid module = %+v, %v; want module core", f, err)
	}
}

func TestSetFeatureParent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child, err := CreateFeature(repoDir, projDir, "Filha", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}
	head0, _ := gitback.HeadSHA(origin)

	if err := SetFeatureParent(repoDir, projDir, child.ID, epic.ID); err != nil {
		t.Fatalf("SetFeatureParent: %v", err)
	}
	got, _ := store.LoadFeature(repoDir, child.ID)
	if got.Parent != epic.ID {
		t.Fatalf("parent = %q, want %q", got.Parent, epic.ID)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "feature.reparented" || last.Payload["to"] != epic.ID || last.Payload["from"] != "" {
		t.Fatalf("event = %+v, want feature.reparented from '' to %s", last, epic.ID)
	}
	if h, _ := gitback.HeadSHA(origin); h == head0 {
		t.Fatalf("origin HEAD did not advance after reparent")
	}

	// Clear the parent.
	if err := SetFeatureParent(repoDir, projDir, child.ID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = store.LoadFeature(repoDir, child.ID)
	if got.Parent != "" {
		t.Fatalf("parent after clear = %q, want empty", got.Parent)
	}

	// Idempotent no-op: setting the same (empty) parent again does not error.
	if err := SetFeatureParent(repoDir, projDir, child.ID, ""); err != nil {
		t.Fatalf("no-op clear: %v", err)
	}
}

func TestSetFeatureParentErrors(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a, err := CreateFeature(repoDir, projDir, "A", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature A: %v", err)
	}
	b, err := CreateFeature(repoDir, projDir, "B", "", a.ID, nil, nil) // B is a child of A
	if err != nil {
		t.Fatalf("CreateFeature B: %v", err)
	}
	if err := SetFeatureParent(repoDir, projDir, "F-9999", a.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
	if err := SetFeatureParent(repoDir, projDir, a.ID, "F-9999"); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("err = %v, want ErrParentNotFound", err)
	}
	if err := SetFeatureParent(repoDir, projDir, a.ID, a.ID); !errors.Is(err, domain.ErrParentSelf) {
		t.Fatalf("err = %v, want ErrParentSelf", err)
	}
	// Cycle: A under B, while B is already under A.
	if err := SetFeatureParent(repoDir, projDir, a.ID, b.ID); !errors.Is(err, domain.ErrParentCycle) {
		t.Fatalf("err = %v, want ErrParentCycle", err)
	}
}

func TestRenameFeatureLive(t *testing.T) {
	for _, st := range []domain.FeatureState{
		domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment,
	} {
		t.Run(string(st), func(t *testing.T) {
			repoDir, projDir, origin := newFeatureBackend(t)
			f, err := CreateFeature(repoDir, projDir, "Título antigo", "", "", nil, nil)
			if err != nil {
				t.Fatalf("CreateFeature: %v", err)
			}
			f.State = st
			if err := store.SaveFeature(repoDir, f); err != nil {
				t.Fatalf("SaveFeature: %v", err)
			}
			head0, _ := gitback.HeadSHA(origin)

			if err := RenameFeature(repoDir, projDir, f.ID, "  Título novo  "); err != nil {
				t.Fatalf("RenameFeature: %v", err)
			}
			got, _ := store.LoadFeature(repoDir, f.ID)
			if got.Title != "Título novo" {
				t.Fatalf("title = %q, want trimmed 'Título novo'", got.Title)
			}
			evs, _ := store.ReadEvents(repoDir)
			last := evs[len(evs)-1]
			if last.Type != "feature.renamed" || last.EntityID != f.ID ||
				last.Payload["from"] != "Título antigo" || last.Payload["to"] != "Título novo" {
				t.Fatalf("event = %+v, want feature.renamed from 'Título antigo' to 'Título novo'", last)
			}
			if h, _ := gitback.HeadSHA(origin); h == head0 {
				t.Fatalf("origin HEAD did not advance after rename")
			}
		})
	}
}

func TestRenameFeatureTerminalFrozen(t *testing.T) {
	for _, st := range []domain.FeatureState{
		domain.StateImplemented, domain.StateRejected, domain.StateSuperseded,
	} {
		t.Run(string(st), func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			f, err := CreateFeature(repoDir, projDir, "Congelado", "", "", nil, nil)
			if err != nil {
				t.Fatalf("CreateFeature: %v", err)
			}
			f.State = st
			if err := store.SaveFeature(repoDir, f); err != nil {
				t.Fatalf("SaveFeature: %v", err)
			}
			if err := RenameFeature(repoDir, projDir, f.ID, "Tentativa"); !errors.Is(err, ErrTitleFrozen) {
				t.Fatalf("err = %v, want ErrTitleFrozen", err)
			}
			got, _ := store.LoadFeature(repoDir, f.ID)
			if got.Title != "Congelado" {
				t.Fatalf("title = %q, want unchanged 'Congelado'", got.Title)
			}
		})
	}
}

func TestRenameFeatureEmpty(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeature(repoDir, projDir, "Original", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if err := RenameFeature(repoDir, projDir, f.ID, "   "); err == nil {
		t.Fatalf("want error for empty title")
	}
	got, _ := store.LoadFeature(repoDir, f.ID)
	if got.Title != "Original" {
		t.Fatalf("title = %q, want unchanged 'Original'", got.Title)
	}
}

func TestRenameFeatureUnchangedNoop(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	f, err := CreateFeature(repoDir, projDir, "Estável", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	head0, _ := gitback.HeadSHA(origin)
	evs0, _ := store.ReadEvents(repoDir)
	if err := RenameFeature(repoDir, projDir, f.ID, "  Estável  "); err != nil {
		t.Fatalf("no-op rename: %v", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin HEAD advanced on unchanged rename")
	}
	evs1, _ := store.ReadEvents(repoDir)
	if len(evs1) != len(evs0) {
		t.Fatalf("events = %d, want unchanged %d on no-op", len(evs1), len(evs0))
	}
}

func TestRenameFeatureUnknown(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := RenameFeature(repoDir, projDir, "F-9999", "X"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

func TestSaveFeatureDoc(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feature", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	head0, _ := gitback.HeadSHA(origin)

	content := "---\ncerne_id: F-0001\n---\n\n# Spec real\n\nProblema, objetivo, critérios.\n"
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", content); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil || string(data) != content {
		t.Fatalf("spec not written: %v / %q", err, string(data))
	}
	// A real spec now counts for the approve gate.
	ds, err := FeatureDocSet(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("FeatureDocSet: %v", err)
	}
	if !ds.HasSpec {
		t.Fatalf("written spec should count as real content")
	}
	head1, _ := gitback.HeadSHA(origin)
	if head1 == head0 {
		t.Fatalf("origin HEAD did not advance after doc save")
	}
	// Idempotent: a byte-identical write is a no-op (no new commit).
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", content); err != nil {
		t.Fatalf("idempotent SaveFeatureDoc: %v", err)
	}
	if head2, _ := gitback.HeadSHA(origin); head2 != head1 {
		t.Fatalf("origin HEAD moved on a byte-identical doc save")
	}
}

func TestSaveFeatureDocErrors(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feature", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "notes", "x"); !errors.Is(err, ErrUnknownDoc) {
		t.Fatalf("err = %v, want ErrUnknownDoc", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-9999", "spec", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

// TestCreateFeatureTags pins tag normalization on create (D-0003): facets are
// lowercased/deduped and validated against the closed set; components are
// slugified/deduped; an unknown facet aborts before minting an id.
func TestCreateFeatureTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)

	f, err := CreateFeature(repoDir, projDir, "Feat", "", "",
		[]string{"infra", "Infra", "code"}, []string{"Gitback", "gitback"})
	if err != nil {
		t.Fatalf("CreateFeature with tags: %v", err)
	}
	if got := f.Facets; len(got) != 2 || got[0] != "infra" || got[1] != "code" {
		t.Fatalf("Facets = %v, want [infra code] (lowered, deduped)", got)
	}
	if got := f.Components; len(got) != 1 || got[0] != "gitback" {
		t.Fatalf("Components = %v, want [gitback] (slugged, deduped)", got)
	}
	// Persisted, not just returned.
	if got, _ := store.LoadFeature(repoDir, "F-0001"); len(got.Facets) != 2 || len(got.Components) != 1 {
		t.Fatalf("persisted tags = facets %v comps %v", got.Facets, got.Components)
	}

	// An unknown facet aborts without consuming the next id.
	if _, err := CreateFeature(repoDir, projDir, "Bad", "", "", []string{"bogus"}, nil); !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want ErrUnknownFacet", err)
	}
	if _, err := store.LoadFeature(repoDir, "F-0002"); err == nil {
		t.Fatalf("F-0002 minted despite invalid facet")
	}
}

// TestRetagFeature pins RetagFeature: per-axis update via the set flags, facet
// validation, and the gate (retaggable while live/implemented, frozen once
// superseded/rejected).
func TestRetagFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feat", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	// Set facets only — components stay empty.
	f, err := RetagFeature(repoDir, projDir, "F-0001", []string{"code"}, nil, true, false)
	if err != nil {
		t.Fatalf("retag facets: %v", err)
	}
	if len(f.Facets) != 1 || f.Facets[0] != "code" || len(f.Components) != 0 {
		t.Fatalf("after facet retag = facets %v comps %v, want [code] / []", f.Facets, f.Components)
	}
	// Set components only — facets preserved.
	f, err = RetagFeature(repoDir, projDir, "F-0001", nil, []string{"gitback"}, false, true)
	if err != nil {
		t.Fatalf("retag components: %v", err)
	}
	if len(f.Facets) != 1 || f.Facets[0] != "code" || len(f.Components) != 1 || f.Components[0] != "gitback" {
		t.Fatalf("after comp retag = facets %v comps %v, want [code] / [gitback]", f.Facets, f.Components)
	}
	// Clear facets (empty slice with set=true).
	f, err = RetagFeature(repoDir, projDir, "F-0001", nil, nil, true, false)
	if err != nil {
		t.Fatalf("clear facets: %v", err)
	}
	if len(f.Facets) != 0 || len(f.Components) != 1 {
		t.Fatalf("after clear = facets %v comps %v, want [] / [gitback]", f.Facets, f.Components)
	}
	// Unknown facet leaves the feature untouched.
	if _, err := RetagFeature(repoDir, projDir, "F-0001", []string{"bogus"}, nil, true, false); !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want ErrUnknownFacet", err)
	}

	// Gate: a rejected feature is frozen history — retag refused.
	rej := store.Feature{ID: "F-0002", Title: "Morta", State: domain.StateRejected}
	if err := store.SaveFeature(repoDir, rej); err != nil {
		t.Fatalf("SaveFeature rejected: %v", err)
	}
	if _, err := RetagFeature(repoDir, projDir, "F-0002", []string{"code"}, nil, true, false); err == nil {
		t.Fatalf("retag of rejected feature succeeded, want error")
	}
}
