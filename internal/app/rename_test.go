package app

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
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
