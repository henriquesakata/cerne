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

	f, err := CreateFeature(repoDir, projDir, "  Minha feature  ", "", "")
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

func TestCreateFeatureEmptyTitle(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "   ", "", ""); err == nil {
		t.Fatalf("want error for empty title")
	}
}

func TestCreateFeatureWithParent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "")
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child, err := CreateFeature(repoDir, projDir, "Filha", "", epic.ID)
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
	if _, err := CreateFeature(repoDir, projDir, "X", "", "F-9999"); !errors.Is(err, ErrParentNotFound) {
		t.Fatalf("err = %v, want ErrParentNotFound", err)
	}
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "")
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	epic.State = domain.StateRejected // drive it terminal directly
	if err := store.SaveFeature(repoDir, epic); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := CreateFeature(repoDir, projDir, "Filha", "", epic.ID); !errors.Is(err, ErrParentTerminal) {
		t.Fatalf("err = %v, want ErrParentTerminal", err)
	}
}

func TestCreateFeatureModuleRules(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t, "core", "web")

	if _, err := CreateFeature(repoDir, projDir, "X", "", ""); err == nil {
		t.Fatalf("want error: --module required with 2+ declared modules")
	}
	if _, err := CreateFeature(repoDir, projDir, "X", "nope", ""); err == nil {
		t.Fatalf("want error: undeclared module")
	}
	f, err := CreateFeature(repoDir, projDir, "X", "core", "")
	if err != nil || f.Module != "core" {
		t.Fatalf("CreateFeature valid module = %+v, %v; want module core", f, err)
	}
}

func TestSetFeatureParent(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	epic, err := CreateFeature(repoDir, projDir, "Épico", "", "")
	if err != nil {
		t.Fatalf("CreateFeature epic: %v", err)
	}
	child, err := CreateFeature(repoDir, projDir, "Filha", "", "")
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
	a, err := CreateFeature(repoDir, projDir, "A", "", "")
	if err != nil {
		t.Fatalf("CreateFeature A: %v", err)
	}
	b, err := CreateFeature(repoDir, projDir, "B", "", a.ID) // B is a child of A
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

func TestSaveFeatureDoc(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feature", "", ""); err != nil {
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
	if _, err := CreateFeature(repoDir, projDir, "Feature", "", ""); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "notes", "x"); !errors.Is(err, ErrUnknownDoc) {
		t.Fatalf("err = %v, want ErrUnknownDoc", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-9999", "spec", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}
