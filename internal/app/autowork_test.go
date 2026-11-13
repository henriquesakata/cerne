package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedActiveWork persists (and pushes) an active work referencing ref.
func seedActiveWork(t *testing.T, repoDir, id, actor, ref string, auto bool) {
	t.Helper()
	now := time.Now().UTC()
	w := store.Work{ID: id, Title: "Atividade " + id, State: domain.WorkActive, Actor: actor, Refs: []string{ref}, Auto: auto, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

// seedStartEvent logs (and pushes) a feature.started for featureID at at:
// the last one is where planningWorks cuts (F-0341).
func seedStartEvent(t *testing.T, repoDir, featureID string, at time.Time) {
	t.Helper()
	ev := domain.NewEvent(featureID, "feature.started", nil, "test")
	ev.At = at
	if err := store.AppendEvent(repoDir, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed start "+featureID); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func loadWorks(t *testing.T, repoDir string) []store.Work {
	t.Helper()
	ws, err := store.LoadAllWorks(repoDir)
	if err != nil {
		t.Fatalf("LoadAllWorks: %v", err)
	}
	return ws
}

// TestAutoWorkOpensOnStart (F-0146 crit. 1): `feature start` auto-opens an
// active work carrying the feature title, ref and default actor, in the same
// transition commit, with the work.started event marked via=auto.
func TestAutoWorkOpensOnStart(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 {
		t.Fatalf("works = %d, want 1 auto-opened", len(ws))
	}
	w := ws[0]
	if w.State != domain.WorkActive || !w.Auto || len(w.Refs) != 1 || w.Refs[0] != "F-0001" {
		t.Fatalf("auto work = %+v, want active auto ref F-0001", w)
	}
	if w.Title != "Feature F-0001" {
		t.Fatalf("title = %q, want the feature title", w.Title)
	}
	if w.Actor == "" {
		t.Fatalf("actor empty, want CERNE_ACTOR ?? user@hostname")
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var found bool
	for _, ev := range evs {
		if ev.Type == "work.started" && ev.EntityID == w.ID && ev.Payload["via"] == "auto" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing work.started via=auto event: %+v", evs)
	}
}

// TestAutoWorkSkipsWhenActorAlreadyOnIt (crit. 3): an existing active work by
// the same actor referencing the feature suppresses the auto-open. The work is
// the pickup auto-work a plan push opens (F-0208 crit. 1): a manual one is a
// planning work, which the start closes (F-0341).
func TestAutoWorkSkipsWhenActorAlreadyOnIt(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", true)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 1 {
		t.Fatalf("works = %d, want 1 (no duplicate)", len(ws))
	}
}

// TestAutoWorkClosedOnComplete (crit. 2): completing the feature closes its
// auto work as done in the same commit, event marked via=auto.
func TestAutoWorkClosedOnComplete(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", true)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || ws[0].State != domain.WorkDone {
		t.Fatalf("work = %+v, want done", ws)
	}
	evs, _ := store.ReadEvents(repoDir)
	var closed bool
	for _, ev := range evs {
		if ev.Type == "work.done" && ev.EntityID == "W-0001" && ev.Payload["via"] == "auto" {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("missing work.done via=auto event")
	}
}

// TestAutoWorkAbandonedOnSupersede (crit. 2): superseding an in-development
// feature abandons its auto work.
func TestAutoWorkAbandonedOnSupersede(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate, "spec.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", true)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionSupersede, "F-0002", "substituída", nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || ws[0].State != domain.WorkAbandoned {
		t.Fatalf("work = %+v, want abandoned", ws)
	}
}

// TestAutoWorkLeavesManualOpen (crit. 5): the automatic close never touches a
// manually opened work (Auto=false), even when it references the feature. The
// work is opened during development — after the feature.started — so it is
// not a planning work, which leaving the cycle closes (F-0341).
func TestAutoWorkLeavesManualOpen(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	seedStartEvent(t, repoDir, "F-0001", time.Now().UTC().Add(-time.Hour))
	seedActiveWork(t, repoDir, "W-0001", "outro@host", "F-0001", false)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || ws[0].State != domain.WorkActive {
		t.Fatalf("manual work = %+v, want still active", ws)
	}
}

// TestOpenAutoWorkForPushOpensOnApprovedPlan (F-0208 crit. 1): pushing a
// plan.md that belongs to a still-approved feature opens the pickup auto-work
// — the first backend touch after claiming the feature, ahead of `feature
// start`.
func TestOpenAutoWorkForPushOpensOnApprovedPlan(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")

	changes := []FileChange{{Path: "content/features/F-0001/plan.md", Content: []byte("plano")}}
	if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
		t.Fatalf("OpenAutoWorkForPush: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || !ws[0].Auto || ws[0].State != domain.WorkActive || ws[0].Refs[0] != "F-0001" {
		t.Fatalf("works = %+v, want 1 active auto ref F-0001", ws)
	}
}

// TestOpenAutoWorkForPushSkipsWhenNotApproved (F-0208 crit. 1): a plan.md push
// for a feature already in_development (e.g. re-editing the plan after
// `feature start`) never opens a duplicate — `feature start` already covers it.
func TestOpenAutoWorkForPushSkipsWhenNotApproved(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")

	changes := []FileChange{{Path: "content/features/F-0001/plan.md", Content: []byte("plano")}}
	if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
		t.Fatalf("OpenAutoWorkForPush: %v", err)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 0 {
		t.Fatalf("works = %d, want 0 (feature not approved)", len(ws))
	}
}

// TestOpenAutoWorkForPushSkipsWhenActorAlreadyOnIt (F-0208 crit. 1/4): a
// manual pickup work already registered by the same actor suppresses the
// auto-open — same dedup as openAutoWork, so pickup -> push never duplicates.
func TestOpenAutoWorkForPushSkipsWhenActorAlreadyOnIt(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", false)

	changes := []FileChange{{Path: "content/features/F-0001/plan.md", Content: []byte("plano")}}
	if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
		t.Fatalf("OpenAutoWorkForPush: %v", err)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 1 {
		t.Fatalf("works = %d, want 1 (no duplicate)", len(ws))
	}
}

// TestOpenAutoWorkForPushOptOut (F-0208 crit. 5): [activity] auto = false
// disables the pickup auto-open too, the same switch F-0146 uses.
func TestOpenAutoWorkForPushOptOut(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	cernerc := "project_id = \"demo\"\nbackend = \"b\"\ndefault_scope = \"\"\n[activity]\nauto = false\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")

	changes := []FileChange{{Path: "content/features/F-0001/plan.md", Content: []byte("plano")}}
	if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
		t.Fatalf("OpenAutoWorkForPush: %v", err)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 0 {
		t.Fatalf("works = %d, want 0 (opt-out)", len(ws))
	}
}

// TestAutoWorkAdoptsManualPickupOnComplete (F-0208 crit. 2): a manual,
// single-ref work opened by the same actor at pickup is closed alongside the
// feature completion, same as an auto work — it never becomes stale-work
// litter.
func TestAutoWorkAdoptsManualPickupOnComplete(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", false)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || ws[0].State != domain.WorkDone {
		t.Fatalf("work = %+v, want done (adopted)", ws)
	}
}

// TestAutoWorkLeavesMultiRefManualOpen (F-0208 crit. 2): a manual work by the
// SAME actor is only adopted when its refs are exactly [featureID] — a
// broader-scoped work (multiple refs) may still cover other in-flight work,
// so completion leaves it untouched.
func TestAutoWorkLeavesMultiRefManualOpen(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	now := time.Now().UTC()
	w := store.Work{ID: "W-0001", Title: "Multi", State: domain.WorkActive, Actor: autoWorkActor(), Refs: []string{"F-0001", "F-0002"}, Auto: false, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed W-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || ws[0].State != domain.WorkActive {
		t.Fatalf("multi-ref manual work = %+v, want still active", ws)
	}
}

// TestAutoWorkOptOut (crit. 7): [activity] auto = false disables the
// auto-open; the transition itself is unaffected.
func TestAutoWorkOptOut(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	cernerc := "project_id = \"demo\"\nbackend = \"b\"\ndefault_scope = \"\"\n[activity]\nauto = false\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if out.To != domain.StateInDevelopment {
		t.Fatalf("transition To = %s, want in_development", out.To)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 0 {
		t.Fatalf("works = %d, want 0 (opt-out)", len(ws))
	}
}

// bindTestSession binds session sess to work id the way `work start` does:
// <cache>/sessions/<sess> holds the id; the cache is repoDir's parent.
func bindTestSession(t *testing.T, repoDir, sess, id string) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(repoDir), "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sess), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pushPlan(t *testing.T, repoDir, projDir string) {
	t.Helper()
	changes := []FileChange{{Path: "content/features/F-0001/plan.md", Content: []byte("plano")}}
	if err := OpenAutoWorkForPush(repoDir, projDir, changes); err != nil {
		t.Fatalf("OpenAutoWorkForPush: %v", err)
	}
}

// TestOpenAutoWorkForPushSkipsW0366Case (F-0383 crit. 4): the TL's manual
// eight-ref work under <user>@<host>, bound to the session, then a plan push
// under CERNE_ACTOR=agent opens nothing.
func TestOpenAutoWorkForPushSkipsW0366Case(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_SESSION", "s1")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	now := time.Now().UTC()
	w := store.Work{ID: "W-0366", Title: "lote", State: domain.WorkActive, Actor: defaultWorkActor(),
		Refs: []string{"F-0001", "F-0002", "F-0003", "F-0004", "F-0005", "F-0006", "F-0007", "F-0008"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatal(err)
	}
	bindTestSession(t, repoDir, "s1", "W-0366")

	pushPlan(t, repoDir, projDir)
	if ws := loadWorks(t, repoDir); len(ws) != 1 {
		t.Fatalf("works = %d, want 1 (no spurious auto work)", len(ws))
	}
}

// TestOpenAutoWorkForPushSkipsBoundWorkOfOtherActor (F-0383 crit. 1): the
// session binding alone suppresses, whatever the work's actor.
func TestOpenAutoWorkForPushSkipsBoundWorkOfOtherActor(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	t.Setenv("CERNE_SESSION", "s1")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", "someone@else", "F-0001", false)
	bindTestSession(t, repoDir, "s1", "W-0001")

	pushPlan(t, repoDir, projDir)
	if ws := loadWorks(t, repoDir); len(ws) != 1 {
		t.Fatalf("works = %d, want 1", len(ws))
	}
}

// TestAutoWorkSkipsOtherOwnIdentity (F-0383 crit. 2): a work under my other
// identity suppresses with no binding, on the push and the start path.
func TestAutoWorkSkipsOtherOwnIdentity(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)

	pushPlan(t, repoDir, projDir)
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if err := openAutoWork(repoDir, f); err != nil {
		t.Fatal(err)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 1 {
		t.Fatalf("works = %d, want 1", len(ws))
	}
}

// TestOpenAutoWorkForPushOpensForForeignUnboundWork (F-0383 crit. 3): another
// actor's work, not bound to this session, does not suppress.
func TestOpenAutoWorkForPushOpensForForeignUnboundWork(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	t.Setenv("CERNE_SESSION", "s1")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", "someone@else", "F-0001", false)

	pushPlan(t, repoDir, projDir)
	if ws := loadWorks(t, repoDir); len(ws) != 2 {
		t.Fatalf("works = %d, want 2", len(ws))
	}
}

// TestOpenAutoWorkForPushOpensWhenBindingIsDone (F-0383 crit. 1): a binding to
// a finished work covers nothing.
func TestOpenAutoWorkForPushOpensWhenBindingIsDone(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	t.Setenv("CERNE_SESSION", "s1")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	now := time.Now().UTC()
	w := store.Work{ID: "W-0001", Title: "x", State: domain.WorkDone, Actor: "someone@else", Refs: []string{"F-0001"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatal(err)
	}
	bindTestSession(t, repoDir, "s1", "W-0001")

	pushPlan(t, repoDir, projDir)
	if ws := loadWorks(t, repoDir); len(ws) != 2 {
		t.Fatalf("works = %d, want 2", len(ws))
	}
}

// TestOpenAutoWorkForPushIgnoresUnusableBinding (F-0383 L2): with no
// CERNE_SESSION a stray binding file suppresses nothing, and a binding that
// points at an F- id (not a work) suppresses nothing either.
func TestOpenAutoWorkForPushIgnoresUnusableBinding(t *testing.T) {
	for name, tc := range map[string]struct{ sess, bound string }{
		"empty session": {"", "W-0001"},
		"binding to F-": {"s1", "F-0001"},
	} {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			t.Setenv("CERNE_SESSION", tc.sess)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
			seedActiveWork(t, repoDir, "W-0001", "someone@else", "F-0001", false)
			bindTestSession(t, repoDir, "s1", tc.bound)

			pushPlan(t, repoDir, projDir)
			if ws := loadWorks(t, repoDir); len(ws) != 2 {
				t.Fatalf("works = %d, want 2", len(ws))
			}
		})
	}
}
