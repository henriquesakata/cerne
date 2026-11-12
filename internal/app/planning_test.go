package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
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
		work("W-0008", 0, false, domain.WorkActive, "F-0003"),            // F-0003 past approved, no start logged
	}
	for feature, want := range map[string][]string{"F-0001": {"W-0001", "W-0002"}, "F-0002": {"W-0007"}, "F-0003": {}} {
		state := domain.StateApproved
		if feature == "F-0003" {
			state = domain.StateInDevelopment
		}
		got, err := planningWorks(dir, works, feature, state)
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
	pinMachine(t, "alice", "m1")
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
	if auto.State != domain.WorkActive || !auto.Auto || auto.Actor != "agent:alice@m1" {
		t.Fatalf("auto work = %+v, want active auto by agent:alice@m1", auto)
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

// TestPlanningClosedOnExit (crit. 2, 6 and 7): leaving the live cycle closes
// the planning works still active — done on complete, abandoned on reject or
// supersede, from candidate, approved or in_development — in the transition's
// commit. A manual work opened during development (after the start) stays, as
// does everything when the feature leaves from a terminal state or the
// project opted out.
func TestPlanningClosedOnExit(t *testing.T) {
	cases := []struct {
		name   string
		from   domain.FeatureState
		action domain.Action
		optOut bool
		want   domain.WorkState // of the planning work; active = untouched
		note   string
	}{
		{"complete from in_development", domain.StateInDevelopment, domain.ActionComplete, false, domain.WorkDone, "feature complete"},
		{"supersede from in_development", domain.StateInDevelopment, domain.ActionSupersede, false, domain.WorkAbandoned, "feature supersede"},
		{"supersede from approved", domain.StateApproved, domain.ActionSupersede, false, domain.WorkAbandoned, "feature supersede"},
		{"supersede from candidate", domain.StateCandidate, domain.ActionSupersede, false, domain.WorkAbandoned, "feature supersede"},
		{"reject from candidate", domain.StateCandidate, domain.ActionReject, false, domain.WorkAbandoned, "feature reject"},
		{"supersede from implemented", domain.StateImplemented, domain.ActionSupersede, false, domain.WorkActive, ""},
		{"opt-out", domain.StateInDevelopment, domain.ActionComplete, true, domain.WorkActive, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			if tc.optOut {
				optOutActivity(t, projDir)
			}
			seedFeatureInState(t, repoDir, "F-0001", tc.from, "spec.md", "plan.md", "result.md")
			seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate, "spec.md")
			seedActiveWork(t, repoDir, "W-0001", "outro@host", "F-0001", false)
			developed := tc.from == domain.StateInDevelopment || tc.from == domain.StateImplemented
			if developed {
				seedStartEvent(t, repoDir, "F-0001", time.Now().UTC())
				seedActiveWork(t, repoDir, "W-0002", "outro@host", "F-0001", false)
			}
			before, err := gitback.HeadSHA(repoDir)
			if err != nil {
				t.Fatalf("HeadSHA: %v", err)
			}

			by, reason := "", ""
			switch tc.action {
			case domain.ActionSupersede:
				by, reason = "F-0002", "substituída"
			case domain.ActionReject:
				reason = "fora de escopo"
			}
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", tc.action, by, reason, nil); err != nil {
				t.Fatalf("%s: %v", tc.action, err)
			}
			if tc.want == domain.WorkActive {
				assertActive(t, repoDir, "W-0001")
			} else {
				assertClosedAuto(t, repoDir, "W-0001", tc.want, tc.note)
				assertOneCommit(t, repoDir, before, "entities/works/W-0001.json", "entities/features/F-0001.json")
			}
			if developed {
				assertActive(t, repoDir, "W-0002") // opened during development (crit. 6)
			}
		})
	}
}

// TestPlanningLegacyWithoutStart: a feature already past approved with no
// feature.started in the log (legacy or imported) has no planning works —
// nothing tells its manual works apart from development ones — so neither its
// exit nor a work start closes them; the work-outlived-feature lint is the net.
func TestPlanningLegacyWithoutStart(t *testing.T) {
	for _, action := range []domain.Action{domain.ActionComplete, domain.ActionSupersede} {
		t.Run(string(action), func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
			seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate, "spec.md")
			seedActiveWork(t, repoDir, "W-0001", "outro@host", "F-0001", false)
			by, reason := "", ""
			if action == domain.ActionSupersede {
				by, reason = "F-0002", "substituída"
			}
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", action, by, reason, nil); err != nil {
				t.Fatalf("%s: %v", action, err)
			}
			assertActive(t, repoDir, "W-0001")
		})
	}
	t.Run("work start", func(t *testing.T) {
		t.Setenv("CERNE_SESSION", "sess-me")
		repoDir, projDir, _ := newFeatureBackend(t)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")
		seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
		if err := BindSession(filepath.Dir(repoDir), "sess-me", "W-0001"); err != nil {
			t.Fatalf("BindSession: %v", err)
		}
		w, err := StartWork(repoDir, projDir, "Revisão da feature legada", []string{"F-0001"}, "", false, nil, "")
		if err != nil {
			t.Fatalf("StartWork: %v", err)
		}
		if len(w.Superseded) != 0 || len(w.Kept) != 0 {
			t.Fatalf("superseded %v kept %v, want none: not a planning work", w.Superseded, w.Kept)
		}
		assertActive(t, repoDir, "W-0001")
	})
}

// TestAutoWorkAdoptionSparesBoundWork (crit. 3, the owner's decision of
// 2026-11-11 applied): the F-0208 adoption by the work's own identity
// (autoWorkActor) works as before, except for a work another session is
// bound to. Adoption by the process's OTHER identity — this feature's
// widening — needs proof: the work bound to this very session. A
// <user>@<host> work a human opened in a terminal (no binding) stays when an
// agent process completes the feature. Every work is opened after the start,
// so only the adoption (not the planning rule) can close it.
func TestAutoWorkAdoptionSparesBoundWork(t *testing.T) {
	cases := []struct {
		name, cerneActor, mySession, boundSession string
		held                                      bool
	}{
		{"same identity, unbound", "", "sess-me", "", false},
		{"same identity, no CERNE_SESSION", "", "", "", false},
		{"same identity, bound to this session", "", "sess-me", "sess-me", false},
		{"same identity, bound to another session", "", "sess-me", "sess-other", true},
		{"same identity, no CERNE_SESSION, any binding holds", "", "", "sess-other", true},
		{"other identity, bound to this session", "agent", "sess-me", "sess-me", false},
		{"other identity, unbound", "agent", "sess-me", "", true},
		{"other identity, no CERNE_SESSION", "agent", "", "", true},
		{"other identity, bound to another session", "agent", "sess-me", "sess-other", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", tc.cerneActor)
			t.Setenv("CERNE_SESSION", tc.mySession)
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
			seedStartEvent(t, repoDir, "F-0001", time.Now().UTC().Add(-time.Hour))
			seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
			if tc.boundSession != "" {
				if err := BindSession(filepath.Dir(repoDir), tc.boundSession, "W-0001"); err != nil {
					t.Fatalf("BindSession: %v", err)
				}
			}

			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if tc.held {
				assertActive(t, repoDir, "W-0001")
				return
			}
			assertClosedAuto(t, repoDir, "W-0001", domain.WorkDone, "feature complete (adotado)")
		})
	}
}

// TestStartWorkSupersedesPlanning (crit. 4): the session re-registering — a
// new work on F-0001 from the session bound to its planning work — closes
// that planning work as done, superseded by the new one, in the work start's
// commit, and reports it.
func TestStartWorkSupersedesPlanning(t *testing.T) {
	t.Setenv("CERNE_SESSION", "sess-me")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
	if err := BindSession(filepath.Dir(repoDir), "sess-me", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	before, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "")
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if w.ID != "W-0002" || !slices.Equal(w.Superseded, []string{"W-0001"}) || len(w.Kept) != 0 {
		t.Fatalf("started = %s superseded %v kept %v, want W-0002 superseding [W-0001]", w.ID, w.Superseded, w.Kept)
	}
	assertClosedAuto(t, repoDir, "W-0001", domain.WorkDone, "superseded by W-0002")
	assertActive(t, repoDir, "W-0002")
	assertOneCommit(t, repoDir, before, "entities/works/W-0001.json", "entities/works/W-0002.json")
}

// TestStartWorkPlanningBindings (crit. 4 and 5, owner's decision of
// 2026-11-11): only a planning work bound to this very session closes. One of
// the same actor that is unbound, bound to another session, or met with no
// CERNE_SESSION stays active and comes back to be warned about — the agent
// actor is the same on every machine, and "unbound" does not prove the
// session ended.
func TestStartWorkPlanningBindings(t *testing.T) {
	// want: which list W-0001 lands in — closed (Superseded), kept unbound
	// (Kept, warned with the closing command) or held by another session
	// (Held, warned without it: that may be live work).
	cases := []struct{ name, mySession, boundSession, want string }{
		{"bound to this session", "sess-me", "sess-me", "closed"},
		{"unbound", "sess-me", "", "kept"},
		{"bound to another session", "sess-me", "sess-other", "held"},
		{"no CERNE_SESSION", "", "", "kept"},
		{"no CERNE_SESSION, bound to some session", "", "sess-other", "held"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_SESSION", tc.mySession)
			repoDir, projDir, _ := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
			seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
			if tc.boundSession != "" {
				if err := BindSession(filepath.Dir(repoDir), tc.boundSession, "W-0001"); err != nil {
					t.Fatalf("BindSession: %v", err)
				}
			}

			w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "")
			if err != nil {
				t.Fatalf("StartWork: %v", err)
			}
			lists := map[string][]string{"closed": w.Superseded, "kept": w.Kept, "held": w.Held}
			for name, ids := range lists {
				if want := name == tc.want; want != slices.Equal(ids, []string{"W-0001"}) || (!want && len(ids) != 0) {
					t.Fatalf("superseded %v kept %v held %v, want W-0001 %s only", w.Superseded, w.Kept, w.Held, tc.want)
				}
			}
			if tc.want != "closed" {
				assertActive(t, repoDir, "W-0001")
				return
			}
			assertClosedAuto(t, repoDir, "W-0001", domain.WorkDone, "superseded by W-0002")
		})
	}
}

// TestStartWorkLeavesNonPlanning (crit. 4, 6 and 7): work start never closes
// another actor's planning work, a work with two refs, an auto work, a work
// opened after the feature started, nor a work on a decision; and nothing at
// all when the project opted out.
func TestStartWorkLeavesNonPlanning(t *testing.T) {
	t.Setenv("CERNE_SESSION", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateInDevelopment, "spec.md", "plan.md")
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "ADR", State: domain.DecisionActive}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	me := defaultWorkActor()
	seedActiveWork(t, repoDir, "W-0001", "outro@host", "F-0001", false) // another actor
	seedActiveWork(t, repoDir, "W-0002", me, "F-0001", true)            // auto
	seedStartEvent(t, repoDir, "F-0002", time.Now().UTC())
	seedActiveWork(t, repoDir, "W-0003", me, "F-0002", false) // opened during development
	seedActiveWork(t, repoDir, "W-0004", me, "D-0001", false) // a decision, not a feature
	now := time.Now().UTC()
	multi := store.Work{ID: "W-0005", Title: "Duas refs", State: domain.WorkActive, Actor: me, Refs: []string{"F-0001", "F-0002"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(repoDir, multi); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed W-0005"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001", "F-0002", "D-0001"}, "", false, nil, "")
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if len(w.Superseded) != 0 || len(w.Kept) != 0 {
		t.Fatalf("superseded %v kept %v, want none", w.Superseded, w.Kept)
	}
	for _, id := range []string{"W-0001", "W-0002", "W-0003", "W-0004", "W-0005"} {
		assertActive(t, repoDir, id)
	}

	t.Run("opt-out", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		optOutActivity(t, projDir)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
		seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
		w, err := StartWork(repoDir, projDir, "Retomada do plano noutra sessão", []string{"F-0001"}, "", false, nil, "")
		if err != nil {
			t.Fatalf("StartWork: %v", err)
		}
		if len(w.Superseded) != 0 {
			t.Fatalf("superseded %v, want none (opt-out)", w.Superseded)
		}
		assertActive(t, repoDir, "W-0001")
	})
}

// TestWorkOutlivedFeatureAfterRealTransitions (crit. 8): the lint's cut reads
// the events the real transitions write — complete, accept, reject and
// supersede through the app — so a renamed event fails here instead of
// silently muting the warning. Opted out, so no transition closes a work
// itself: each one opened before its feature closed is flagged, the one
// opened after is not.
func TestWorkOutlivedFeatureAfterRealTransitions(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "") // accept is a human gate
	repoDir, projDir, _ := newFeatureBackend(t)
	optOutActivity(t, projDir)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate, "spec.md")
	seedFeatureInState(t, repoDir, "F-0003", domain.StateApproved, "spec.md", "plan.md")
	seedFeatureInState(t, repoDir, "F-0004", domain.StateImplemented, "spec.md", "plan.md", "result.md")
	seedFeatureInState(t, repoDir, "F-0005", domain.StateCandidate, "spec.md")
	for i, f := range []string{"F-0001", "F-0002", "F-0003", "F-0004"} {
		seedActiveWork(t, repoDir, fmt.Sprintf("W-%04d", i+1), "outro@host", f, false)
	}
	for _, tr := range []struct {
		id         string
		action     domain.Action
		by, reason string
	}{
		{"F-0001", domain.ActionComplete, "", ""},
		{"F-0002", domain.ActionReject, "", "fora de escopo"},
		{"F-0003", domain.ActionSupersede, "F-0005", "substituída"},
		{"F-0004", domain.ActionAccept, "", "validado"},
	} {
		if _, err := TransitionFeature(repoDir, projDir, tr.id, tr.action, tr.by, tr.reason, nil); err != nil {
			t.Fatalf("%s %s: %v", tr.action, tr.id, err)
		}
	}
	seedActiveWork(t, repoDir, "W-0005", "outro@host", "F-0001", false) // errata, after the complete

	findings, err := lint.RunConfig(repoDir, 14, 8)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var flagged []string
	for _, f := range findings {
		if f.Rule == lint.RuleWorkOutlivedFeature {
			flagged = append(flagged, f.Entity)
		}
	}
	slices.Sort(flagged)
	if want := []string{"W-0001", "W-0002", "W-0003", "W-0004"}; !slices.Equal(flagged, want) {
		t.Fatalf("work-outlived-feature = %v, want %v", flagged, want)
	}
}
