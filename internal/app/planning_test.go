package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0341: the planning work closes when the feature moves on. Tests pin the
// spec's criteria by number.

// workIDs lists the ids of ws, in order.
func workIDs(ws []store.Work) []string {
	out := []string{}
	for _, w := range ws {
		out = append(out, w.ID)
	}
	return out
}

// assertOneCommit checks HEAD is exactly one commit past before and that the
// commit touches every path: the closures land with the transition.
func assertOneCommit(t *testing.T, repoDir, before string, paths ...string) {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "rev-list", "--count", before+"..HEAD").Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if n := strings.TrimSpace(string(out)); n != "1" {
		t.Fatalf("commits since %s = %s, want 1", before, n)
	}
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	changed, err := gitback.ChangedPaths(repoDir, before, head)
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	for _, p := range paths {
		if !slices.Contains(changed, p) {
			t.Fatalf("commit misses %s: %v", p, changed)
		}
	}
}

// assertClosedAuto checks work id ended in state with note, and that its
// closing event carries the note and via=auto.
func assertClosedAuto(t *testing.T, repoDir, id string, state domain.WorkState, note string) {
	t.Helper()
	w, err := store.LoadWork(repoDir, id)
	if err != nil {
		t.Fatalf("LoadWork %s: %v", id, err)
	}
	if w.State != state || w.Note != note {
		t.Fatalf("%s = %s %q, want %s %q", id, w.State, w.Note, state, note)
	}
	evType := "work.done"
	if state == domain.WorkAbandoned {
		evType = "work.abandoned"
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == evType && ev.Payload["via"] == "auto" && ev.Payload["note"] == note {
			return
		}
	}
	t.Fatalf("missing %s via=auto with note %q for %s", evType, note, id)
}

// assertActive checks work id is still active.
func assertActive(t *testing.T, repoDir, id string) {
	t.Helper()
	w, err := store.LoadWork(repoDir, id)
	if err != nil {
		t.Fatalf("LoadWork %s: %v", id, err)
	}
	if w.State != domain.WorkActive {
		t.Fatalf("%s = %s %q, want active", id, w.State, w.Note)
	}
}

// optOutActivity writes a .cernerc with [activity] auto = false.
func optOutActivity(t *testing.T, projDir string) {
	t.Helper()
	cernerc := "project_id = \"demo\"\nbackend = \"b\"\ndefault_scope = \"\"\n[activity]\nauto = false\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
}

// TestPlanningWorks (the definition + crit. 6): a planning work is active,
// manual, refs exactly [F-X] and was created before F-X's last
// feature.started — or at any time when F-X never started. A feature that
// started, came back and started again makes the work opened between the two
// starts a planning work of the second.
func TestPlanningWorks(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for _, ev := range []domain.Event{
		{ID: "e1", At: t0.Add(1 * time.Hour), EntityID: "F-0001", Type: "feature.started"},
		{ID: "e2", At: t0.Add(3 * time.Hour), EntityID: "F-0001", Type: "feature.started"},
		{ID: "e3", At: t0.Add(5 * time.Hour), EntityID: "F-0001", Type: "feature.completed"},
	} {
		if err := store.AppendEvent(dir, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	work := func(id string, created time.Duration, auto bool, state domain.WorkState, refs ...string) store.Work {
		return store.Work{ID: id, State: state, Actor: "a@b", Refs: refs, Auto: auto, CreatedAt: t0.Add(created)}
	}
	works := []store.Work{
		work("W-0001", 0, false, domain.WorkActive, "F-0001"),            // before both starts
		work("W-0002", 2*time.Hour, false, domain.WorkActive, "F-0001"),  // between the starts
		work("W-0003", 4*time.Hour, false, domain.WorkActive, "F-0001"),  // after the last start
		work("W-0004", 0, false, domain.WorkActive, "F-0001", "F-0002"),  // two refs
		work("W-0005", 0, true, domain.WorkActive, "F-0001"),             // auto
		work("W-0006", 0, false, domain.WorkDone, "F-0001"),              // already closed
		work("W-0007", 10*time.Hour, false, domain.WorkActive, "F-0002"), // F-0002 never started
	}
	for feature, want := range map[string][]string{"F-0001": {"W-0001", "W-0002"}, "F-0002": {"W-0007"}} {
		got, err := planningWorks(dir, works, feature)
		if err != nil {
			t.Fatalf("planningWorks %s: %v", feature, err)
		}
		if ids := workIDs(got); !slices.Equal(ids, want) {
			t.Errorf("planningWorks %s = %v, want %v", feature, ids, want)
		}
	}
}

// TestPlanningClosedOnStart (crit. 1): the start closes every planning work of
// the feature, of any actor — the TL's <user>@<host> one and a foreign one the
// forced start passed — as done, in the transition's commit, and the auto work
// still opens.
func TestPlanningClosedOnStart(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", "outro@host", "F-0001", false)
	seedActiveWork(t, repoDir, "W-0002", defaultWorkActor(), "F-0001", false)
	before, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	if _, err := TransitionFeatureForce(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil, true); err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, id := range []string{"W-0001", "W-0002"} {
		assertClosedAuto(t, repoDir, id, domain.WorkDone, "planning closed: feature start")
	}
	auto, err := store.LoadWork(repoDir, "W-0003")
	if err != nil {
		t.Fatalf("auto work: %v", err)
	}
	if auto.State != domain.WorkActive || !auto.Auto || auto.Actor != "agent" {
		t.Fatalf("auto work = %+v, want active auto by agent", auto)
	}
	assertOneCommit(t, repoDir, before, "entities/works/W-0001.json", "entities/works/W-0002.json", "entities/works/W-0003.json", "entities/features/F-0001.json")
}

// TestPlanningClosedOnRestart (the TL's re-entry case): a feature that
// already started once and is approved again has a feature.started in the
// log; the work opened after it is a planning work of the new start.
func TestPlanningClosedOnRestart(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedStartEvent(t, repoDir, "F-0001", time.Now().UTC().Add(-2*time.Hour))
	seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertClosedAuto(t, repoDir, "W-0001", domain.WorkDone, "planning closed: feature start")
}

// TestPlanningOptOutOnStart (crit. 7): [activity] auto = false keeps the
// planning work open across the start.
func TestPlanningOptOutOnStart(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	optOutActivity(t, projDir)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	assertActive(t, repoDir, "W-0001")
}
