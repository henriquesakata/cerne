package app

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedRename saves entities of every kind, in live and closed states, eight of
// them carrying "old"; F-0003 carries only "gitea" and must stay untouched.
func seedRename(t *testing.T, repoDir string) {
	t.Helper()
	for _, err := range []error{
		store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Done feature", State: domain.FeatureState("done"), Components: []string{"old", "gitea"}}),
		store.SaveFeature(repoDir, store.Feature{ID: "T-0002", Title: "Rejected task", State: domain.FeatureState("rejected"), Components: []string{"old"}}),
		store.SaveFeature(repoDir, store.Feature{ID: "F-0003", Title: "Live feature", State: domain.FeatureState("in_development"), Components: []string{"gitea"}}),
		store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "Old note", State: domain.NoteState("superseded"), Components: []string{"kpack", "old"}}),
		store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Old decision", State: domain.DecisionState("superseded"), Components: []string{"old"}}),
		store.SaveReference(repoDir, store.Reference{ID: "R-0001", Title: "Old ref", State: domain.ReferenceState("obsolete"), Components: []string{"old"}}),
		store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Runbook", State: domain.RunbookState("active"), Components: []string{"old"}}),
		store.SavePending(repoDir, store.Pending{ID: "P-0001", Title: "Pending", From: "F-0001", State: domain.PendingState("resolved"), Components: []string{"old"}}),
		store.SaveIdea(repoDir, store.Idea{ID: "I-0001", Title: "Idea", State: domain.IdeaState("dropped"), Components: []string{"old"}}),
	} {
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	commitBackend(t, repoDir)
}

// componentsOf reads every entity's components, keyed by id.
func componentsOf(t *testing.T, repoDir string) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	fs, _ := store.LoadAllFeatures(repoDir)
	for _, x := range fs {
		got[x.ID] = x.Components
	}
	ns, _ := store.LoadAllNotes(repoDir)
	for _, x := range ns {
		got[x.ID] = x.Components
	}
	ds, _ := store.LoadAllDecisions(repoDir)
	for _, x := range ds {
		got[x.ID] = x.Components
	}
	rs, _ := store.LoadAllReferences(repoDir)
	for _, x := range rs {
		got[x.ID] = x.Components
	}
	rbs, _ := store.LoadAllRunbooks(repoDir)
	for _, x := range rbs {
		got[x.ID] = x.Components
	}
	ps, _ := store.LoadAllPendings(repoDir)
	for _, x := range ps {
		got[x.ID] = x.Components
	}
	is, _ := store.LoadAllIdeas(repoDir)
	for _, x := range is {
		got[x.ID] = x.Components
	}
	return got
}

func eventCount(t *testing.T, repoDir string) int {
	t.Helper()
	evs, err := store.ReadAllEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	return len(evs)
}

// F-0482 criterion 7: one write renames the component on every kind and state,
// keeps the order of the other components, and logs one retag event per entity.
func TestRenameComponentOneWriteEveryKindAndState(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedRename(t, repoDir)
	commits, events := commitCount(t, repoDir), eventCount(t, repoDir)

	rep, err := RenameComponent(repoDir, projDir, "old", "fresh", false, false)
	if err != nil {
		t.Fatalf("RenameComponent: %v", err)
	}
	if got := commitCount(t, repoDir); got != commits+1 {
		t.Fatalf("commits %d -> %d, want exactly one more", commits, got)
	}
	if rep.Merge || rep.ToCount != 0 || rep.DryRun || len(rep.Entities) != 8 {
		t.Fatalf("report = %+v, want 8 entities, a new name, written", rep)
	}
	want := map[string][]string{
		"F-0001": {"fresh", "gitea"}, "T-0002": {"fresh"}, "F-0003": {"gitea"},
		"N-0001": {"kpack", "fresh"}, "D-0001": {"fresh"}, "R-0001": {"fresh"},
		"RB-0001": {"fresh"}, "P-0001": {"fresh"}, "I-0001": {"fresh"},
	}
	got := componentsOf(t, repoDir)
	for id, w := range want {
		if !slices.Equal(got[id], w) {
			t.Errorf("%s components = %v, want %v", id, got[id], w)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != "done" {
		t.Errorf("F-0001 state = %s, want done (a rename changes only tags)", f.State)
	}

	all, _ := store.ReadAllEvents(repoDir)
	if len(all) != events+8 {
		t.Fatalf("events %d -> %d, want one per renamed entity (8)", events, len(all))
	}
	kinds := map[string]string{"F-0001": "feature", "T-0002": "feature", "N-0001": "note", "D-0001": "decision",
		"R-0001": "reference", "RB-0001": "runbook", "P-0001": "pending", "I-0001": "idea"}
	for _, ev := range all[events:] {
		if ev.Type != kinds[ev.EntityID]+".retagged" {
			t.Errorf("event %s %s, want %s.retagged", ev.EntityID, ev.Type, kinds[ev.EntityID])
		}
		if ev.Payload["components"] != strings.Join(want[ev.EntityID], ",") {
			t.Errorf("event %s components = %q, want %q", ev.EntityID, ev.Payload["components"], strings.Join(want[ev.EntityID], ","))
		}
		delete(kinds, ev.EntityID)
	}
	if len(kinds) != 0 {
		t.Errorf("no retag event for %v", kinds)
	}
}

// A rename to a component already in use is a merge: no check, and an entity
// that already had it does not end up with it twice.
func TestRenameComponentMergesIntoExisting(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedRename(t, repoDir)
	rep, err := RenameComponent(repoDir, projDir, "old", "gitea", false, false)
	if err != nil {
		t.Fatalf("RenameComponent: %v", err)
	}
	if !rep.Merge || rep.ToCount != 2 {
		t.Fatalf("report merge=%v to_count=%d, want a merge into the 2 entities carrying gitea", rep.Merge, rep.ToCount)
	}
	got := componentsOf(t, repoDir)
	if !slices.Equal(got["F-0001"], []string{"gitea"}) || !slices.Equal(got["T-0002"], []string{"gitea"}) {
		t.Fatalf("F-0001 %v, T-0002 %v; want [gitea] each, no duplicate", got["F-0001"], got["T-0002"])
	}
}

// Refusals and the dry run write nothing: HEAD, events and tags as they were.
func TestRenameComponentRefusalsAndDryRunWriteNothing(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		force, dryRun  bool
		wantErr        error
		wantEntities   int
	}{
		{name: "from absent", from: "absent", to: "fresh", wantErr: ErrComponentNotFound},
		{name: "same after normalizing", from: "old", to: "Old", wantErr: ErrSameComponent},
		{name: "to near an existing one", from: "old", to: "kpak", wantErr: ErrNearComponent},
		{name: "near name refused on a dry run too", from: "old", to: "kpak", dryRun: true, wantErr: ErrNearComponent},
		{name: "dry run", from: "old", to: "fresh", dryRun: true, wantEntities: 8},
		{name: "dry run of a merge", from: "old", to: "gitea", dryRun: true, wantEntities: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedRename(t, repoDir)
			commits, events, tags := commitCount(t, repoDir), eventCount(t, repoDir), componentsOf(t, repoDir)
			rep, err := RenameComponent(repoDir, projDir, tc.from, tc.to, tc.force, tc.dryRun)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(rep.Entities) != tc.wantEntities || (err == nil && !rep.DryRun) {
				t.Fatalf("report = %+v, want %d entities on a dry run", rep, tc.wantEntities)
			}
			if commitCount(t, repoDir) != commits || eventCount(t, repoDir) != events {
				t.Fatalf("wrote: commits %d -> %d, events %d -> %d", commits, commitCount(t, repoDir), events, eventCount(t, repoDir))
			}
			for id, c := range componentsOf(t, repoDir) {
				if !slices.Equal(c, tags[id]) {
					t.Errorf("%s components %v -> %v", id, tags[id], c)
				}
			}
			if out, _ := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output(); len(out) != 0 {
				t.Errorf("worktree dirty after a write-nothing rename:\n%s", out)
			}
		})
	}
}

// --force writes a name near an existing one; a name near only the component
// being renamed away is no near-duplicate (that one disappears).
func TestRenameComponentNearNameCheck(t *testing.T) {
	for _, tc := range []struct {
		name, to string
		force    bool
	}{
		{name: "force passes a near name", to: "kpak", force: true},
		{name: "near only the renamed-away component", to: "olds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedRename(t, repoDir)
			rep, err := RenameComponent(repoDir, projDir, "old", tc.to, tc.force, false)
			if err != nil || len(rep.Entities) != 8 {
				t.Fatalf("rename to %q: %v, %d entities", tc.to, err, len(rep.Entities))
			}
			if got := componentsOf(t, repoDir)["T-0002"]; !slices.Equal(got, []string{tc.to}) {
				t.Fatalf("T-0002 components = %v, want [%s]", got, tc.to)
			}
		})
	}
}

// M1 (critic round 1): a save that fails mid-way (the features are written
// and their events appended, then the notes directory refuses the write)
// leaves the backend as it was: HEAD, events, tags and a clean worktree.
func TestRenameComponentMidwayFailureWritesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only directory")
	}
	repoDir, projDir, _ := newFeatureBackend(t)
	seedRename(t, repoDir)
	commits, events, tags := commitCount(t, repoDir), eventCount(t, repoDir), componentsOf(t, repoDir)
	notes := filepath.Join(repoDir, "entities", "notes")
	if err := os.Chmod(notes, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(notes, 0o755) })

	if _, err := RenameComponent(repoDir, projDir, "old", "fresh", false, false); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want the notes save to fail", err)
	}
	if commitCount(t, repoDir) != commits || eventCount(t, repoDir) != events {
		t.Fatalf("wrote: commits %d -> %d, events %d -> %d", commits, commitCount(t, repoDir), events, eventCount(t, repoDir))
	}
	for id, c := range componentsOf(t, repoDir) {
		if !slices.Equal(c, tags[id]) {
			t.Errorf("%s components %v -> %v: the saves before the failure stayed", id, tags[id], c)
		}
	}
	if out, _ := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output(); len(out) != 0 {
		t.Errorf("worktree dirty after a failed rename:\n%s", out)
	}
}

// M2 (critic round 1): both names go through the normalizer every component
// writer uses, so a mixed-case or punctuated spelling finds the stored name
// and writes the name a retag would.
func TestRenameComponentNormalizesLikeTheWriters(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0005", Title: "n", State: domain.NoteState("active"),
		Components: domain.NormalizeComponents([]string{"Auth_Tokens", "kpack"})}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	rep, err := RenameComponent(repoDir, projDir, "Auth_Tokens", "ui.v2", false, false)
	if err != nil {
		t.Fatalf("RenameComponent: %v", err)
	}
	from, to := domain.NormalizeComponents([]string{"Auth_Tokens"}), domain.NormalizeComponents([]string{"ui.v2"})
	if rep.From != from[0] || rep.To != to[0] || len(rep.Entities) != 1 {
		t.Fatalf("report %q -> %q on %d entities, want %q -> %q on 1", rep.From, rep.To, len(rep.Entities), from[0], to[0])
	}
	if got, want := componentsOf(t, repoDir)["N-0005"], domain.NormalizeComponents([]string{"ui.v2", "kpack"}); !slices.Equal(got, want) {
		t.Fatalf("N-0005 components = %v, want %v", got, want)
	}
}

// M3 (critic round 1): a target carried only by a closed idea, a resolved
// pending or a superseded note is in use: a merge, counted, and not refused as
// near "kpack" (which it would be, were it new).
func TestRenameComponentMergesIntoTargetOnlyOnClosedEntities(t *testing.T) {
	for _, tc := range []struct {
		name string
		save func(repoDir string) error
	}{
		{"dropped idea", func(r string) error {
			return store.SaveIdea(r, store.Idea{ID: "I-0002", Title: "i", State: domain.IdeaState("dropped"), Components: []string{"kpak"}})
		}},
		{"resolved pending", func(r string) error {
			return store.SavePending(r, store.Pending{ID: "P-0002", Title: "p", From: "F-0001", State: domain.PendingState("resolved"), Components: []string{"kpak"}})
		}},
		{"superseded note", func(r string) error {
			return store.SaveNote(r, store.Note{ID: "N-0002", Title: "n", State: domain.NoteState("superseded"), Components: []string{"kpak"}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			if err := tc.save(repoDir); err != nil {
				t.Fatal(err)
			}
			seedRename(t, repoDir)
			rep, err := RenameComponent(repoDir, projDir, "old", "kpak", false, false)
			if err != nil {
				t.Fatalf("RenameComponent: %v", err)
			}
			if !rep.Merge || rep.ToCount != 1 || len(rep.Entities) != 8 {
				t.Fatalf("merge=%v to_count=%d entities=%d, want a merge into 1 entity, 8 renamed", rep.Merge, rep.ToCount, len(rep.Entities))
			}
		})
	}
}

// pushFromOtherClone tags N-0099 with "old" from a second clone of origin, so
// the local clone trails the backend by one entity carrying the component.
func pushFromOtherClone(t *testing.T, origin string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.SaveNote(other, store.Note{ID: "N-0099", Title: "Remote note", State: domain.NoteState("active"), Components: []string{"old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(other, "remote: tag N-0099"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

// M4 (critic round 1): the dry run and the write read the same tree, the
// backend's latest, so the dry run lists what the apply will change.
func TestRenameComponentReadsTheBackendLatest(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry run", false: "write"}[dryRun], func(t *testing.T) {
			repoDir, projDir, origin := newFeatureBackend(t)
			seedRename(t, repoDir)
			pushFromOtherClone(t, origin)
			rep, err := RenameComponent(repoDir, projDir, "old", "fresh", false, dryRun)
			if err != nil {
				t.Fatalf("RenameComponent: %v", err)
			}
			if len(rep.Entities) != 9 || !slices.ContainsFunc(rep.Entities, func(e RenamedEntity) bool { return e.ID == "N-0099" }) {
				t.Fatalf("listed %d entities, want 9 with N-0099, only on origin: %+v", len(rep.Entities), rep.Entities)
			}
			if !dryRun && !slices.Equal(componentsOf(t, repoDir)["N-0099"], []string{"fresh"}) {
				t.Fatalf("N-0099 components = %v after the write", componentsOf(t, repoDir)["N-0099"])
			}
		})
	}
}
