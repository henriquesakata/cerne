package app

import (
	"errors"
	"fmt"
	"sync"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/plantmpl"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
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
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Minha feature", "", "", nil, nil, body, false, "", "")
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
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Sem corpo", "", "", nil, nil, "   ", false, "", "")
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

// TestCreateFeatureOwnerPriority pins F-0204's criterion 1: owner/priority
// persist on creation, and an invalid priority is refused with nothing
// written (no id minted, no event).
func TestCreateFeatureOwnerPriority(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Com dono", "", "", nil, nil, "", false, "  ana  ", "P1")
	if err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	if f.Owner != "ana" || f.Priority != "p1" {
		t.Fatalf("feature = %+v, want Owner=ana Priority=p1 (trimmed/lowercased)", f)
	}
	got, err := store.LoadFeature(repoDir, f.ID)
	if err != nil || got.Owner != "ana" || got.Priority != "p1" {
		t.Fatalf("LoadFeature = %+v, %v; want persisted owner/priority", got, err)
	}
}

func TestCreateFeatureInvalidPriorityWritesNothing(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Prioridade ruim", "", "", nil, nil, "", false, "", "p9"); !errors.Is(err, domain.ErrInvalidPriority) {
		t.Fatalf("err = %v, want ErrInvalidPriority", err)
	}
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	if len(feats) != 0 {
		t.Fatalf("feats = %+v, want nothing written on invalid priority", feats)
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
	// Creation starts from a clean HEAD (F-0121): the state flip must be
	// committed to be visible to the guard.
	if _, err := gitback.CommitAndPush(repoDir, "set state"); err != nil {
		t.Fatalf("CommitAndPush state: %v", err)
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

// TestFeatureParentStoredCanonical (F-0326 crit. 1): the epic link is written
// canonical whatever the caller typed — on create, on reparent, in the events —
// and the "unchanged" check of a reparent compares canonically. A
// whitespace-only parent is still refused, never a silent "no epic" or a
// silent clear ("" alone is the API's explicit clear).
func TestFeatureParentStoredCanonical(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	lower := strings.ToLower(epic.ID)

	child, err := CreateFeature(repoDir, projDir, "Filha", "", lower, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}
	if got, _ := store.LoadFeature(repoDir, child.ID); child.Parent != epic.ID || got.Parent != epic.ID {
		t.Fatalf("parent returned %q, persisted %q; want %q", child.Parent, got.Parent, epic.ID)
	}
	evs, _ := store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Type != "feature.created" || last.Payload["parent"] != epic.ID {
		t.Fatalf("event = %+v, want feature.created carrying parent %s", last, epic.ID)
	}

	other, err := CreateFeature(repoDir, projDir, "Avulsa", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature other: %v", err)
	}
	if err := SetFeatureParent(repoDir, projDir, other.ID, " "+lower+" "); err != nil {
		t.Fatalf("SetFeatureParent: %v", err)
	}
	if got, _ := store.LoadFeature(repoDir, other.ID); got.Parent != epic.ID {
		t.Fatalf("reparented parent = %q, want %q", got.Parent, epic.ID)
	}
	evs, _ = store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Type != "feature.reparented" || last.Payload["to"] != epic.ID {
		t.Fatalf("event = %+v, want feature.reparented to %s", last, epic.ID)
	}

	head, _ := gitback.HeadSHA(origin)
	if err := SetFeatureParent(repoDir, projDir, other.ID, lower); err != nil {
		t.Fatalf("same parent in another case: %v", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head {
		t.Fatalf("reparent to the same epic in another case committed; want a no-op")
	}

	if _, err := CreateFeature(repoDir, projDir, "Branca", "", "  ", nil, nil); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("blank parent on create: err = %v, want ErrParentNotFound", err)
	}
	if err := SetFeatureParent(repoDir, projDir, other.ID, "  "); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("blank parent on reparent: err = %v, want ErrParentNotFound", err)
	}
	if got, _ := store.LoadFeature(repoDir, other.ID); got.Parent != epic.ID {
		t.Fatalf("blank reparent changed the parent to %q, want %q kept", got.Parent, epic.ID)
	}
}

// TestReparentRefusesCycleThroughLegacyParent (F-0326 crit. 2): a parent
// stored in another case before the canonical write still closes a cycle.
func TestReparentRefusesCycleThroughLegacyParent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature A: %v", err)
	}
	b, err := CreateFeature(repoDir, projDir, "Filha", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature B: %v", err)
	}
	b.Parent = strings.ToLower(a.ID) // legacy data: B under A, written lowercase
	if err := store.SaveFeature(repoDir, b); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "legacy lowercase parent"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	if err := SetFeatureParent(repoDir, projDir, a.ID, b.ID); !errors.Is(err, domain.ErrParentCycle) {
		t.Fatalf("err = %v, want ErrParentCycle", err)
	}
	if got, _ := store.LoadFeature(repoDir, a.ID); got.Parent != "" {
		t.Fatalf("A.Parent = %q after a refused cycle, want empty", got.Parent)
	}
}

// TestFeatureParentRefusesPathShapedID (F-0326): the parent is an entity id,
// not a path. A value that only resolves as a file path ("./F-0001",
// "F-0001/../F-0001") is refused before anything is written, instead of being
// stored as a parent no reader resolves.
func TestFeatureParentRefusesPathShapedID(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child, err := CreateFeature(repoDir, projDir, "Avulsa", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}
	for _, bad := range []string{"./" + strings.ToLower(epic.ID), epic.ID + "/../" + epic.ID} {
		if _, err := CreateFeature(repoDir, projDir, "Travessia "+bad, "", bad, nil, nil); !errors.Is(err, ErrParentNotFound) {
			t.Fatalf("create with parent %q: err = %v, want ErrParentNotFound", bad, err)
		}
		if err := SetFeatureParent(repoDir, projDir, child.ID, bad); !errors.Is(err, ErrParentNotFound) {
			t.Fatalf("reparent to %q: err = %v, want ErrParentNotFound", bad, err)
		}
	}
	all, _ := store.LoadAllFeatures(repoDir)
	if len(all) != 2 {
		t.Fatalf("features = %d, want 2 (refused creations write nothing)", len(all))
	}
	if got, _ := store.LoadFeature(repoDir, child.ID); got.Parent != "" {
		t.Fatalf("child parent = %q after refused reparents, want empty", got.Parent)
	}
}

// TestSetFeatureParentCanonicalChildID (F-0326): the child id is canonical
// too, on any filesystem: the reparent applies and its event is recorded
// under the stored id, so readers keyed by F-NNNN never miss it.
func TestSetFeatureParentCanonicalChildID(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child, err := CreateFeature(repoDir, projDir, "Avulsa", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}
	if err := SetFeatureParent(repoDir, projDir, " "+strings.ToLower(child.ID)+" ", epic.ID); err != nil {
		t.Fatalf("SetFeatureParent with a lowercase child id: %v", err)
	}
	if got, _ := store.LoadFeature(repoDir, child.ID); got.Parent != epic.ID {
		t.Fatalf("parent = %q, want %q", got.Parent, epic.ID)
	}
	evs, _ := store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Type != "feature.reparented" || last.EntityID != child.ID {
		t.Fatalf("event = %+v, want feature.reparented on %s", last, child.ID)
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
			commitBackend(t, repoDir)
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

// TestSaveFeatureDocEmitsEvent pins the audit trail of F-0262: every effective
// doc write appends feature.doc_edited carrying the document and the feature's
// state at write time; a byte-identical rewrite appends nothing.
func TestSaveFeatureDocEmitsEvent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feature", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	spec := "---\ncerne_id: F-0001\n---\n\n# Spec real\n\nProblema, objetivo, critérios.\n" + testBarTail
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", spec); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", spec+"\nAjuste pós-aprovação.\n"); err != nil {
		t.Fatalf("SaveFeatureDoc after approve: %v", err)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "feature.doc_edited" || last.EntityID != "F-0001" ||
		last.Payload["doc"] != "spec" || last.Payload["state"] != string(domain.StateApproved) {
		t.Fatalf("last event = %+v, want feature.doc_edited{doc:spec,state:approved}", last)
	}
	// The first (candidate) write is recorded too, with its own state.
	var candidateEdits int
	for _, ev := range evs {
		if ev.Type == "feature.doc_edited" && ev.Payload["state"] == string(domain.StateCandidate) {
			candidateEdits++
		}
	}
	if candidateEdits != 1 {
		t.Fatalf("candidate doc_edited events = %d, want 1", candidateEdits)
	}
	// Byte-identical rewrite: no new event.
	before := len(evs)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", spec+"\nAjuste pós-aprovação.\n"); err != nil {
		t.Fatalf("idempotent SaveFeatureDoc: %v", err)
	}
	evs, _ = store.ReadEvents(repoDir)
	if len(evs) != before {
		t.Fatalf("events = %d after byte-identical write, want %d", len(evs), before)
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

// TestSetFeature pins F-0204's criterion 2: SetFeature mutates owner/priority
// in place via the set flags, validates priority before writing, and refuses
// on a terminal (frozen) feature.
func TestSetFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feat", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	// Set owner only — priority stays empty.
	f, err := SetFeature(repoDir, projDir, "F-0001", "ana", "", "", true, false, false)
	if err != nil {
		t.Fatalf("set owner: %v", err)
	}
	if f.Owner != "ana" || f.Priority != "" {
		t.Fatalf("after owner set = owner %q priority %q, want ana / \"\"", f.Owner, f.Priority)
	}
	// Set priority only — owner preserved.
	f, err = SetFeature(repoDir, projDir, "F-0001", "", "p2", "", false, true, false)
	if err != nil {
		t.Fatalf("set priority: %v", err)
	}
	if f.Owner != "ana" || f.Priority != "p2" {
		t.Fatalf("after priority set = owner %q priority %q, want ana / p2", f.Owner, f.Priority)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "feature.set" || last.EntityID != "F-0001" || last.Payload["priority"] != "p2" {
		t.Fatalf("event = %+v, want feature.set with priority p2", last)
	}
	// Invalid priority leaves the feature untouched.
	if _, err := SetFeature(repoDir, projDir, "F-0001", "", "p9", "", false, true, false); !errors.Is(err, domain.ErrInvalidPriority) {
		t.Fatalf("err = %v, want ErrInvalidPriority", err)
	}
	got, _ := store.LoadFeature(repoDir, "F-0001")
	if got.Priority != "p2" {
		t.Fatalf("priority = %q, want unchanged p2 after invalid set", got.Priority)
	}

	// Gate: a terminal feature (implemented, per domain.TitleEditable) is frozen.
	frozen := store.Feature{ID: "F-0002", Title: "Pronta", State: domain.StateImplemented}
	if err := store.SaveFeature(repoDir, frozen); err != nil {
		t.Fatalf("SaveFeature frozen: %v", err)
	}
	commitBackend(t, repoDir) // MutateAndSnapshot's pre-mutate hard reset drops uncommitted files
	if _, err := SetFeature(repoDir, projDir, "F-0002", "ana", "", "", true, false, false); !errors.Is(err, ErrFeatureFrozen) {
		t.Fatalf("err = %v, want ErrFeatureFrozen", err)
	}
}

// TestCreateFeatureWithFixes pins F-0321's criterion 17 on create: fixes is
// stored on the entity and carried by the feature.created payload; a creation
// without it leaves no fixes key.
func TestCreateFeatureWithFixes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Entregue", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	evs, _ := store.ReadEvents(repoDir)
	if _, ok := evs[len(evs)-1].Payload["fixes"]; ok {
		t.Fatalf("payload without fixes = %v, want no fixes key", evs[len(evs)-1].Payload)
	}
	f, err := CreateFeatureFixes(repoDir, projDir, "Corrige", "", "", "F-0001", nil, nil, "", false, "", "")
	if err != nil {
		t.Fatalf("CreateFeatureFixes: %v", err)
	}
	if f.ID != "F-0002" || f.Fixes != "F-0001" {
		t.Fatalf("feature = %s fixes %q, want F-0002 fixes F-0001", f.ID, f.Fixes)
	}
	got, _ := store.LoadFeature(repoDir, "F-0002")
	if got.Fixes != "F-0001" {
		t.Fatalf("stored fixes = %q, want F-0001", got.Fixes)
	}
	evs, _ = store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "feature.created" || last.EntityID != "F-0002" || last.Payload["fixes"] != "F-0001" {
		t.Fatalf("event = %+v, want feature.created with fixes F-0001", last)
	}
}

// TestSetFeatureFixes pins criterion 17 on set: fixes is written in place with
// owner/priority preserved, and an empty value clears it — both on the payload.
func TestSetFeatureFixes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	for _, title := range []string{"Entregue", "Corrige"} {
		if _, err := CreateFeature(repoDir, projDir, title, "", "", nil, nil); err != nil {
			t.Fatalf("CreateFeature: %v", err)
		}
	}
	if _, err := SetFeature(repoDir, projDir, "F-0002", "ana", "p1", "", true, true, false); err != nil {
		t.Fatalf("set owner/priority: %v", err)
	}
	f, err := SetFeature(repoDir, projDir, "F-0002", "", "", "F-0001", false, false, true)
	if err != nil {
		t.Fatalf("set fixes: %v", err)
	}
	if f.Fixes != "F-0001" || f.Owner != "ana" || f.Priority != "p1" {
		t.Fatalf("after set = fixes %q owner %q priority %q, want F-0001 / ana / p1", f.Fixes, f.Owner, f.Priority)
	}
	evs, _ := store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Type != "feature.set" || last.Payload["fixes"] != "F-0001" {
		t.Fatalf("event = %+v, want feature.set with fixes F-0001", last)
	}
	f, err = SetFeature(repoDir, projDir, "F-0002", "", "", "", false, false, true)
	if err != nil {
		t.Fatalf("clear fixes: %v", err)
	}
	if got, _ := store.LoadFeature(repoDir, "F-0002"); f.Fixes != "" || got.Fixes != "" {
		t.Fatalf("after clear = %q / stored %q, want empty", f.Fixes, got.Fixes)
	}
	evs, _ = store.ReadEvents(repoDir)
	if v, ok := evs[len(evs)-1].Payload["fixes"]; !ok || v != "" {
		t.Fatalf("clear payload = %v, want fixes: \"\"", evs[len(evs)-1].Payload)
	}
}

// TestFixesUnknownFeatureRefused pins criterion 18: --fixes naming a missing
// feature is ErrFixesNotFound and writes nothing; an implemented target (the
// normal case — the defect shipped) is accepted.
func TestFixesUnknownFeatureRefused(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Corrige", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	featsBefore, _ := store.LoadAllFeatures(repoDir)
	evsBefore, _ := store.ReadEvents(repoDir)
	if _, err := CreateFeatureFixes(repoDir, projDir, "Outra", "", "", "F-9999", nil, nil, "", false, "", ""); !errors.Is(err, ErrFixesNotFound) {
		t.Fatalf("create err = %v, want ErrFixesNotFound", err)
	}
	if _, err := SetFeature(repoDir, projDir, "F-0001", "", "", "F-9999", false, false, true); !errors.Is(err, ErrFixesNotFound) {
		t.Fatalf("set err = %v, want ErrFixesNotFound", err)
	}
	featsAfter, _ := store.LoadAllFeatures(repoDir)
	evsAfter, _ := store.ReadEvents(repoDir)
	if len(featsAfter) != len(featsBefore) || len(evsAfter) != len(evsBefore) {
		t.Fatalf("features %d→%d events %d→%d, want nothing written", len(featsBefore), len(featsAfter), len(evsBefore), len(evsAfter))
	}
	if got, _ := store.LoadFeature(repoDir, "F-0001"); got.Fixes != "" {
		t.Fatalf("fixes = %q after refused set, want untouched", got.Fixes)
	}

	done := store.Feature{ID: "F-0002", Title: "Pronta", State: domain.StateImplemented}
	if err := store.SaveFeature(repoDir, done); err != nil {
		t.Fatalf("SaveFeature implemented: %v", err)
	}
	commitBackend(t, repoDir) // MutateAndSnapshot's pre-mutate hard reset drops uncommitted files
	if _, err := CreateFeatureFixes(repoDir, projDir, "Corrige a pronta", "", "", "F-0002", nil, nil, "", false, "", ""); err != nil {
		t.Fatalf("fixes on an implemented feature: %v, want accepted", err)
	}
}

// TestFixesCycleRefused pins C18: a feature cannot fix itself, nor the
// feature that already fixes it — refused with ErrFixesCycle, nothing written.
func TestFixesCycleRefused(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Entregue", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if _, err := CreateFeatureFixes(repoDir, projDir, "Corrige", "", "", "F-0001", nil, nil, "", false, "", ""); err != nil {
		t.Fatalf("CreateFeatureFixes: %v", err)
	}
	evsBefore, _ := store.ReadEvents(repoDir)
	if _, err := SetFeature(repoDir, projDir, "F-0002", "", "", "F-0002", false, false, true); !errors.Is(err, ErrFixesCycle) {
		t.Fatalf("self-reference err = %v, want ErrFixesCycle", err)
	}
	if _, err := SetFeature(repoDir, projDir, "F-0001", "", "", "F-0002", false, false, true); !errors.Is(err, ErrFixesCycle) {
		t.Fatalf("two-cycle err = %v, want ErrFixesCycle", err)
	}
	evsAfter, _ := store.ReadEvents(repoDir)
	f1, _ := store.LoadFeature(repoDir, "F-0001")
	f2, _ := store.LoadFeature(repoDir, "F-0002")
	if len(evsAfter) != len(evsBefore) || f1.Fixes != "" || f2.Fixes != "F-0001" {
		t.Fatalf("events %d→%d, fixes F-0001 %q F-0002 %q, want nothing written", len(evsBefore), len(evsAfter), f1.Fixes, f2.Fixes)
	}
}

// TestCreateFeatureDuplicateTitle pins the F-0121 retry guard: a title whose
// normalized form (domain.NormalizeTitle, the F-0045 rule) matches a LIVE
// feature is refused with ErrDuplicateTitle naming the existing id — nothing
// minted, nothing written. Terminal states never block: history is not a veto
// on new work.
func TestCreateFeatureDuplicateTitle(t *testing.T) {
	cases := []struct {
		state   domain.FeatureState
		wantDup bool
	}{
		{domain.StateCandidate, true},
		{domain.StateApproved, true},
		{domain.StateInDevelopment, true},
		{domain.StateImplemented, false},
		{domain.StateRejected, false},
		{domain.StateSuperseded, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			f, err := CreateFeature(repoDir, projDir, "Minha feature", "", "", nil, nil)
			if err != nil {
				t.Fatalf("seed CreateFeature: %v", err)
			}
			f.State = tc.state
			if err := store.SaveFeature(repoDir, f); err != nil {
				t.Fatalf("SaveFeature: %v", err)
			}
			// candidate keeps the seeded bytes byte-identical: nothing to commit.
			if _, err := gitback.CommitAndPush(repoDir, "set state"); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
				t.Fatalf("CommitAndPush state: %v", err)
			}

			// Normalized-equal title: extra internal whitespace collapses.
			got, err := CreateFeature(repoDir, projDir, "  Minha   feature ", "", "", nil, nil)
			if tc.wantDup {
				if !errors.Is(err, ErrDuplicateTitle) {
					t.Fatalf("err = %v, want ErrDuplicateTitle", err)
				}
				for _, frag := range []string{f.ID, string(tc.state)} {
					if !strings.Contains(err.Error(), frag) {
						t.Errorf("error %q should name %q", err.Error(), frag)
					}
				}
				if _, err := store.LoadFeature(repoDir, "F-0002"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("duplicate was minted anyway: LoadFeature F-0002 err = %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("CreateFeature over terminal title: %v", err)
				}
				if got.ID != "F-0002" {
					t.Fatalf("id = %s, want F-0002", got.ID)
				}
			}
		})
	}
}

// F-0121 crit. 5: the creation starts from a clean tree — leftover uncommitted
// state in the shared clone (a crashed process's write) must not ride into the
// creation commit.
func TestCreateFeatureDoesNotSweepStrayState(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	writeBackendFile(t, repoDir, "content/features/F-0099/spec.md", "half-written by a stranger\n")

	if _, err := CreateFeature(repoDir, projDir, "Criação limpa", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if _, ok := originHas(t, origin, "content/features/F-0099/spec.md"); ok {
		t.Fatal("stray state was swept into the creation commit")
	}
}

// F-0121 crit. 6: N concurrent creations on the same shared clone serialize on
// the backend lock — N features, distinct ids, each spec landing on origin.
func TestCreateFeatureConcurrent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	const n = 4
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f, err := CreateFeatureWithSpec(repoDir, projDir,
				fmt.Sprintf("Concorrente %d", i), "", "", nil, nil,
				fmt.Sprintf("## Problema\n\ncorpo %d\n", i), false, "", "")
			ids[i], errs[i] = f.ID, err
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("create %d: %v", i, errs[i])
		}
		if seen[ids[i]] {
			t.Fatalf("duplicate id minted: %s", ids[i])
		}
		seen[ids[i]] = true
		spec, ok := originHas(t, origin, "content/features/"+ids[i]+"/spec.md")
		if !ok || !strings.Contains(spec, fmt.Sprintf("corpo %d", i)) {
			t.Fatalf("spec of %s on origin: ok=%v content=%q — want the seeded body", ids[i], ok, spec)
		}
	}
}

// F-0213 crit.1: approving an epic (a feature with children already minted)
// scaffolds its plan.md with the execution map built from those children.
func TestApproveEpicWithChildrenScaffoldsExecutionMap(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child1, err := CreateFeature(repoDir, projDir, "Fase 1", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child1: %v", err)
	}
	child2, err := CreateFeature(repoDir, projDir, "Fase 2", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child2: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, epic.ID, "spec", realDoc(epic.ID)); err != nil {
		t.Fatalf("save spec: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, epic.ID, domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve epic: %v", err)
	}

	plan := readFeatureDocFile(t, repoDir, epic.ID, "plan.md")
	for _, want := range []string{
		"Etapa 1: " + child1.ID,
		"Etapa 2: " + child2.ID,
		"TODO: agrupe em etapas",
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("plan.md missing %q:\n%s", want, plan)
		}
	}
}

// F-0213 crit.2: a plain feature (no children) keeps the unchanged plan stub.
func TestApproveFeatureWithoutChildrenPlanUnchanged(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeature(repoDir, projDir, "Feature simples", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, f.ID, "spec", realDoc(f.ID)); err != nil {
		t.Fatalf("save spec: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, f.ID, domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}

	plan := readFeatureDocFile(t, repoDir, f.ID, "plan.md")
	want := "---\ncerne_id: " + f.ID + "\n---\n\n" + plantmpl.Stub(f.Title)
	if plan != want {
		t.Fatalf("plan.md changed for a childless feature:\ngot:\n%s\nwant:\n%s", plan, want)
	}
}

// F-0235 crit.1: a candidate epic gains its plan-with-execution-map the
// moment it gets its first child, not only at approve (F-0213) — the epic is
// never registered decomposed-but-unplanned.
func TestCreateFeatureScaffoldsParentPlan_WhenStub(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	if epic.State != domain.StateCandidate {
		t.Fatalf("epic.State = %s, want candidate", epic.State)
	}

	child, err := CreateFeature(repoDir, projDir, "Fase 1", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}

	plan := readFeatureDocFile(t, repoDir, epic.ID, "plan.md")
	if !strings.Contains(plan, "Etapa 1: "+child.ID) {
		t.Fatalf("plan.md not scaffolded with the new child while parent is still candidate:\n%s", plan)
	}
}

// F-0235 crit.2: a second child arriving while the plan is still a stub
// re-scaffolds the execution map to include it.
func TestCreateFeatureReScaffoldsParentPlan_WithSecondChild(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child1, err := CreateFeature(repoDir, projDir, "Fase 1", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child1: %v", err)
	}
	child2, err := CreateFeature(repoDir, projDir, "Fase 2", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child2: %v", err)
	}

	plan := readFeatureDocFile(t, repoDir, epic.ID, "plan.md")
	for _, want := range []string{"Etapa 1: " + child1.ID, "Etapa 2: " + child2.ID} {
		if !strings.Contains(plan, want) {
			t.Fatalf("plan.md missing %q after second child:\n%s", want, plan)
		}
	}
}

// F-0235: once the parent's plan is filled with real content, further
// children never touch it — "plano preenchido = nunca tocado".
func TestCreateFeatureNeverOverwritesRealParentPlan(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, epic.ID, "plan", realDoc(epic.ID)); err != nil {
		t.Fatalf("save real plan: %v", err)
	}
	want := readFeatureDocFile(t, repoDir, epic.ID, "plan.md")

	if _, err := CreateFeature(repoDir, projDir, "Fase 1", "", epic.ID, nil, nil); err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}

	plan := readFeatureDocFile(t, repoDir, epic.ID, "plan.md")
	if plan != want {
		t.Fatalf("real plan was overwritten by child creation:\ngot:\n%s\nwant:\n%s", plan, want)
	}
}

// F-0235 crit.1: reparenting under a stub-planned epic scaffolds it the same
// way creation does.
func TestSetFeatureParentScaffoldsParentPlan_WhenStub(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	f, err := CreateFeature(repoDir, projDir, "Solta", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature f: %v", err)
	}

	if err := SetFeatureParent(repoDir, projDir, f.ID, epic.ID); err != nil {
		t.Fatalf("SetFeatureParent: %v", err)
	}

	plan := readFeatureDocFile(t, repoDir, epic.ID, "plan.md")
	if !strings.Contains(plan, "Etapa 1: "+f.ID) {
		t.Fatalf("plan.md not scaffolded after reparent:\n%s", plan)
	}
}

// F-0235: EpicPlanStub is the signal the cmd layer prints the approve
// advisory from — true for a fresh/stub plan, false once real content lands,
// false when the doc does not exist at all.
func TestEpicPlanStub(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if EpicPlanStub(repoDir, "F-9999") {
		t.Fatalf("EpicPlanStub = true for a feature whose plan.md does not exist yet, want false")
	}
	f, err := CreateFeature(repoDir, projDir, "Épico", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if _, err := CreateFeature(repoDir, projDir, "Fase 1", "", f.ID, nil, nil); err != nil {
		t.Fatalf("CreateFeature child (scaffolds parent plan.md): %v", err)
	}
	if !EpicPlanStub(repoDir, f.ID) {
		t.Fatalf("EpicPlanStub = false right after scaffold, want true")
	}
	if err := SaveFeatureDoc(repoDir, projDir, f.ID, "plan", realDoc(f.ID)); err != nil {
		t.Fatalf("save real plan: %v", err)
	}
	if EpicPlanStub(repoDir, f.ID) {
		t.Fatalf("EpicPlanStub = true after a real plan was saved, want false")
	}
}

func readFeatureDocFile(t *testing.T, repoDir, id, file string) string {
	t.Helper()
	data, err := os.ReadFile(FeatureDocPath(repoDir, id, file))
	if err != nil {
		t.Fatalf("read %s/%s: %v", id, file, err)
	}
	return string(data)
}

// TestUpdateCandidateSpecIdenticalIsNoOp (F-0328 crit. 4): rewriting the spec
// with what is already stored — the same body, or the stored file itself as
// `cerne show --doc spec` prints it — is a successful no-op: changed false, no
// feature.doc_edited, no commit. A different body still writes and says so.
func TestUpdateCandidateSpecIdenticalIsNoOp(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature", "", "", nil, nil,
		"## Problema\n\nVersão antiga.", false, "", ""); err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	const body = "## Problema\n\nVersão nova.\n"
	if _, changed, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", body); err != nil || !changed {
		t.Fatalf("first rewrite: changed %v, err %v — want a write", changed, err)
	}
	// The rewrite's backend commit is named for what it touched (judge, F-0328).
	if subject := gitOut(t, origin, "log", "-1", "--format=%s"); subject != "cerne: update F-0001/spec.md" {
		t.Fatalf("commit subject = %q, want %q", subject, "cerne: update F-0001/spec.md")
	}
	path := FeatureDocPath(repoDir, "F-0001", "spec.md")
	stored, _ := os.ReadFile(path)
	evs0, _ := store.ReadEvents(repoDir)
	head0, _ := gitback.HeadSHA(origin)

	for name, again := range map[string]string{"same body": body, "stored file as show prints it": string(stored) + "\n"} {
		f, changed, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", again)
		if err != nil || changed || f.ID != "F-0001" || f.State != domain.StateCandidate {
			t.Fatalf("%s: f %s %s, changed %v, err %v — want a successful no-op", name, f.ID, f.State, changed, err)
		}
		if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evs0) {
			t.Fatalf("%s: events = %d, want %d", name, len(evs), len(evs0))
		}
		if h, _ := gitback.HeadSHA(origin); h != head0 {
			t.Fatalf("%s: origin moved: %s -> %s", name, head0, h)
		}
		if cur, _ := os.ReadFile(path); string(cur) != string(stored) {
			t.Fatalf("%s: spec.md = %q, want %q", name, cur, stored)
		}
	}
}

// TestUpdateCandidateSpec (F-0256 criteria 3/5): a revision confirmed in the
// Backlog chat rewrites the candidate's spec through the same gated write path
// as the registration — cerne_id frontmatter restamped, lifecycle state
// untouched, one commit pushed.
func TestUpdateCandidateSpec(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature", "", "", nil, nil,
		"## Problema\n\nVersão antiga.", false, "", ""); err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	head0, _ := gitback.HeadSHA(origin)

	f, _, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", "## Problema\n\nVersão nova.\n")
	if err != nil {
		t.Fatalf("UpdateCandidateSpec: %v", err)
	}
	if f.State != domain.StateCandidate || f.ID != "F-0001" {
		t.Fatalf("feature = %+v, want F-0001 still candidate", f)
	}
	want := "---\ncerne_id: F-0001\n---\n\n## Problema\n\nVersão nova.\n"
	if got := readFeatureDocFile(t, repoDir, "F-0001", "spec.md"); got != want {
		t.Fatalf("spec.md = %q, want %q", got, want)
	}
	if head1, _ := gitback.HeadSHA(origin); head1 == head0 {
		t.Fatalf("origin HEAD did not advance after the spec update")
	}
	stored, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil || stored.State != domain.StateCandidate {
		t.Fatalf("stored = %+v, %v; want state untouched (candidate)", stored, err)
	}
	// F-0265 criterion 9: the write announces itself in the history, with the
	// actor — the same trail the feature-page editor leaves (F-0262).
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "feature.doc_edited" || last.EntityID != "F-0001" ||
		last.Payload["doc"] != "spec" || last.Payload["state"] != string(domain.StateCandidate) ||
		last.Actor == "" {
		t.Fatalf("last event = %+v, want feature.doc_edited{doc:spec,state:candidate} with actor", last)
	}
}

// TestUpdateCandidateSpecRefusals (F-0256 criterion 5, moved past approval by
// F-0318): in development the spec is the contract being built — the chat
// write path refuses it, and nothing on disk changes. An unknown id is
// store.ErrNotFound, like every other doc write.
func TestUpdateCandidateSpecRefusals(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	before := readFeatureDocFile(t, repoDir, "F-0001", "spec.md")

	if _, _, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", "## Problema\n\nBurlado.\n"); !errors.Is(err, ErrNotCandidate) {
		t.Fatalf("err = %v, want ErrNotCandidate", err)
	}
	if got := readFeatureDocFile(t, repoDir, "F-0001", "spec.md"); got != before {
		t.Fatalf("spec.md changed on a refused update: %q", got)
	}
	if _, _, err := UpdateCandidateSpec(repoDir, projDir, "F-9999", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}

// TestUpdateCandidateSpecIdenticalPastApprovalRefused (judge, F-0328): the
// state check comes before the identity check — a feature past approval is
// refused even when the body is exactly its stored spec, never answered with
// a no-op that would read as "accepted, nothing to do".
func TestUpdateCandidateSpecIdenticalPastApprovalRefused(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md")
	stored := readFeatureDocFile(t, repoDir, "F-0001", "spec.md")
	evs0, _ := store.ReadEvents(repoDir)
	head0, _ := gitback.HeadSHA(origin)
	_, changed, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", stored)
	if !errors.Is(err, ErrNotCandidate) || changed {
		t.Fatalf("identical body past approval: changed %v, err %v — want ErrNotCandidate", changed, err)
	}
	if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evs0) {
		t.Fatalf("events = %d, want %d", len(evs), len(evs0))
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved: %s -> %s", head0, h)
	}
}

// TestUpdateCandidateSpecUnchangedIsJudgedAgainstOrigin (F-0328 critic C3): a
// clone that trails origin does not call a body "unchanged" because its stale
// copy matches — the rewrite looks at the backend first, so the body lands on
// origin and says so; only then is the same body a no-op.
func TestUpdateCandidateSpecUnchangedIsJudgedAgainstOrigin(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	const two = "## Problema\n\nVersão dois."
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature", "", "", nil, nil, two, false, "", ""); err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	const rel = "content/features/F-0001/spec.md"
	advanceRemote(t, origin, rel, "---\ncerne_id: F-0001\n---\n\n## Problema\n\nVersão três.\n")

	if _, changed, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", two); err != nil || !changed {
		t.Fatalf("stale clone: changed %v, err %v — want a write, origin holds another spec", changed, err)
	}
	if got, _ := originHas(t, origin, rel); got != "---\ncerne_id: F-0001\n---\n\n"+two+"\n" {
		t.Fatalf("origin spec = %q, want version two", got)
	}
	if _, changed, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", two); err != nil || changed {
		t.Fatalf("now identical on origin: changed %v, err %v — want a no-op", changed, err)
	}
}

// TestSpecBodyOnlyFrontmatterIsEmpty (F-0328 critic C5): a body that is only
// its frontmatter is empty, refused by the core itself — the rewrite keeps the
// spec it had, the creation mints nothing — whatever the caller checked.
func TestSpecBodyOnlyFrontmatterIsEmpty(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Feature", "", "", nil, nil, "## Problema\n\nReal.", false, "", ""); err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	before := readFeatureDocFile(t, repoDir, "F-0001", "spec.md")
	head0, _ := gitback.HeadSHA(origin)
	for _, body := range []string{"", "\n  \n", "---\ncerne_id: F-0001\n---\n", "---\ncerne_id: F-0001\n---\n\n---\ncerne_id: F-0001\n---\n"} {
		if _, _, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", body); !errors.Is(err, ErrEmptyBody) || err.Error() != "empty body: the spec has no content besides its frontmatter" {
			t.Fatalf("rewrite %q: err = %v, want ErrEmptyBody", body, err)
		}
	}
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Outra", "", "", nil, nil, "---\ncerne_id: F-0002\n---\n", false, "", ""); !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("create: err = %v, want ErrEmptyBody", err)
	}
	if got := readFeatureDocFile(t, repoDir, "F-0001", "spec.md"); got != before {
		t.Fatalf("spec.md changed on an empty body: %q", got)
	}
	if features, _ := store.LoadAllFeatures(repoDir); len(features) != 1 {
		t.Fatalf("features = %d, want 1", len(features))
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved on an empty body: %s -> %s", head0, h)
	}
}
