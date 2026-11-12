package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestStartRefusesForeignActiveWork (F-0257 crit. 1 + 5): an active work of
// another actor on the feature refuses the start — no state change, no event,
// and no second auto-work registered.
func TestStartRefusesForeignActiveWork(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", "outro-agente", "F-0001", false)

	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if !errors.Is(err, ErrActiveWorkElsewhere) {
		t.Fatalf("start error = %v, want ErrActiveWorkElsewhere", err)
	}
	if !strings.Contains(err.Error(), "W-0001") || !strings.Contains(err.Error(), "outro-agente") {
		t.Fatalf("refuse must list the colliding work and its actor: %v", err)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved (refused start writes nothing)", f.State)
	}
	if ws := loadWorks(t, repoDir); len(ws) != 1 {
		t.Fatalf("works = %d, want 1 (no auto-work on a refused start)", len(ws))
	}
}

// TestStartForceOverridesForeignWork (crit. 2): --force starts anyway and the
// override is recorded on the feature.started event.
func TestStartForceOverridesForeignWork(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", "outro-agente", "F-0001", false)

	out, err := TransitionFeatureForce(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil, true)
	if err != nil {
		t.Fatalf("forced start: %v", err)
	}
	if out.To != domain.StateInDevelopment {
		t.Fatalf("state = %s, want in_development", out.To)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var forced bool
	for _, ev := range evs {
		if ev.Type == "feature.started" && ev.EntityID == "F-0001" && ev.Payload["guard"] == "forced" {
			forced = true
		}
	}
	if !forced {
		t.Fatalf("missing feature.started with guard=forced: %+v", evs)
	}
}

// TestStartAllowsSameActorWork (crit. 3 + 4): the F-0208 pickup work — same
// actor, no session evidence — warns but never blocks.
func TestStartAllowsSameActorWork(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", false)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if out.To != domain.StateInDevelopment {
		t.Fatalf("state = %s, want in_development", out.To)
	}
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "W-0001") {
		t.Fatalf("warnings = %v, want one advisory naming W-0001", out.Warnings)
	}
}

// TestStartAllowsOwnWorkUnderCerneActor (crit. 3): an agent session runs with
// CERNE_ACTOR set, but `cerne work start` stamps the work with <user>@<host> —
// the guard must recognize both identities as the same session, or the F-0208
// pickup work blocks the very start it was opened for.
func TestStartAllowsOwnWorkUnderCerneActor(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if out.To != domain.StateInDevelopment {
		t.Fatalf("state = %s, want in_development", out.To)
	}
}

// TestClassifyParallelWorks pins the identity rules the guard degrades through
// (crit. 3 and 4) without the backend plumbing.
func TestClassifyParallelWorks(t *testing.T) {
	now := time.Now().UTC()
	work := func(id, actor string) store.Work {
		return store.Work{ID: id, Title: "atividade", State: domain.WorkActive, Actor: actor, Refs: []string{"F-0001"}, CreatedAt: now.Add(-90 * time.Minute)}
	}
	cases := []struct {
		name          string
		works         []store.Work
		session       string
		bindings      map[string]string
		wantForeign   []string
		wantAmbiguous []string
	}{
		{
			name:     "bound to my session is mine",
			works:    []store.Work{work("W-0001", "eu@maquina")},
			session:  "sess-a",
			bindings: map[string]string{"sess-a": "W-0001"},
		},
		{
			name:        "bound to another session on the same machine and actor",
			works:       []store.Work{work("W-0001", "eu@maquina")},
			session:     "sess-a",
			bindings:    map[string]string{"sess-b": "W-0001"},
			wantForeign: []string{"W-0001"},
		},
		{
			name:        "different actor is foreign even without bindings",
			works:       []store.Work{work("W-0001", "outro@maquina")},
			wantForeign: []string{"W-0001"},
		},
		{
			name:          "same actor without session evidence only warns",
			works:         []store.Work{work("W-0001", "eu@maquina")},
			wantAmbiguous: []string{"W-0001"},
		},
		{
			// `work start` stamps <user>@<host> even under CERNE_ACTOR: my own
			// pickup work must not read as another agent's.
			name:          "my other identity is still mine",
			works:         []store.Work{work("W-0001", "agent:eu@maquina")},
			wantAmbiguous: []string{"W-0001"},
		},
		{
			name:        "the same agent user on another machine is foreign (F-0350 crit. 4)",
			works:       []store.Work{work("W-0001", "agent:eu@outra")},
			wantForeign: []string{"W-0001"},
		},
		{
			name:          "a legacy bare agent work is ambiguous, never foreign (F-0350)",
			works:         []store.Work{work("W-0001", "agent")},
			wantAmbiguous: []string{"W-0001"},
		},
		{
			name: "terminal work and other refs never match",
			works: []store.Work{
				{ID: "W-0002", State: domain.WorkDone, Actor: "outro@maquina", Refs: []string{"F-0001"}, CreatedAt: now},
				{ID: "W-0003", State: domain.WorkActive, Actor: "outro@maquina", Refs: []string{"F-0009"}, CreatedAt: now},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			foreign, ambiguous := classifyParallelWorks(tc.works, "F-0001", []string{"eu@maquina", "agent:eu@maquina"}, tc.session, tc.bindings, now)
			if got := ids(foreign); !equal(got, tc.wantForeign) {
				t.Fatalf("foreign = %v, want %v", got, tc.wantForeign)
			}
			if got := ids(ambiguous); !equal(got, tc.wantAmbiguous) {
				t.Fatalf("ambiguous = %v, want %v", got, tc.wantAmbiguous)
			}
		})
	}
}

func ids(ps []parallelWork) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func equal(a, b []string) bool {
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

// TestWorkStartRefuseTellsToStop (crit. 6): the duplicate-guard refuse for a
// work owned by another actor prescribes stopping, not adopting the record.
func TestWorkStartRefuseTellsToStop(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", "outro-agente", "F-0001", false)

	_, err := StartWork(repoDir, projDir, "Atividade W-0001", []string{"F-0001"}, "", false, nil, "")
	if !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("work start error = %v, want ErrSimilarExists", err)
	}
	if !strings.Contains(err.Error(), "stop and confirm") || !strings.Contains(err.Error(), "outro-agente") {
		t.Fatalf("refuse must name the other agent and tell to stop: %v", err)
	}
}

// TestStartSameActorOtherSessionWarnsOnly (F-0325, critic C14): F-0325 now
// exports CERNE_SESSION into every agent session. A same-actor work bound to
// another session id — the same person after /clear or a restart — must stay
// the F-0208 ambiguous warning, not turn into a hard refusal.
func TestStartSameActorOtherSessionWarnsOnly(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: origin}); err != nil {
		t.Fatal(err)
	}
	cacheDir, err := project.CacheDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md", "plan.md")
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "F-0001", false)
	if err := BindSession(cacheDir, "sess-before-clear", "W-0001"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_SESSION", "sess-after-clear")

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("start refused: %v", err)
	}
	if out.To != domain.StateInDevelopment || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "W-0001") {
		t.Fatalf("start = %+v, want in_development with one warning naming W-0001", out)
	}
}
