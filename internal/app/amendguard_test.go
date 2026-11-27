package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestClassifyAmendWorks (F-0485 criteria 1, 4, 5 and 7): the amend guard reads
// the card and each live child, with session evidence on.
func TestClassifyAmendWorks(t *testing.T) {
	now := time.Now().UTC()
	work := func(id, actor, ref string) store.Work {
		return store.Work{ID: id, Title: "atividade", State: domain.WorkActive, Actor: actor, Refs: []string{ref}, CreatedAt: now.Add(-time.Hour)}
	}
	features := []store.Feature{
		{ID: "E-0001", State: domain.StateInDevelopment},
		{ID: "T-0001", Parent: "E-0001", State: domain.StateInDevelopment},
		{ID: "T-0002", Parent: "E-0001", State: domain.StateDone},
	}
	mine := []string{"eu@maquina", "agent:eu@maquina"}
	cases := []struct {
		name          string
		works         []store.Work
		session       string
		bindings      map[string]string
		wantForeign   []string
		wantAmbiguous []string
	}{
		{"another session on the card", []store.Work{work("W-1", "eu@maquina", "E-0001")}, "sess-a", map[string]string{"sess-b": "W-1"}, []string{"W-1"}, nil},
		{"another session on a live child", []store.Work{work("W-1", "eu@maquina", "T-0001")}, "sess-a", map[string]string{"sess-b": "W-1"}, []string{"W-1"}, nil},
		{"another actor", []store.Work{work("W-1", "outro@maquina", "E-0001")}, "sess-a", nil, []string{"W-1"}, nil},
		{"same actor, no binding", []store.Work{work("W-1", "eu@maquina", "E-0001")}, "sess-a", nil, nil, []string{"W-1"}},
		{"my own session", []store.Work{work("W-1", "outro@maquina", "E-0001")}, "sess-a", map[string]string{"sess-a": "W-1"}, nil, nil},
		{"a subagent of my session", []store.Work{work("W-1", "eu@maquina", "T-0001")}, "sess-a", map[string]string{"sess-a.dev": "W-1"}, nil, nil},
		{"a closed work", []store.Work{{ID: "W-1", State: domain.WorkDone, Actor: "outro@maquina", Refs: []string{"E-0001"}}}, "sess-a", nil, nil, nil},
		{"a concluded child", []store.Work{work("W-1", "outro@maquina", "T-0002")}, "sess-a", nil, nil, nil},
		{"no CERNE_SESSION: another session falls back to the same actor", []store.Work{work("W-1", "eu@maquina", "E-0001")}, "", map[string]string{"sess-b": "W-1"}, nil, []string{"W-1"}},
		{"no CERNE_SESSION: another actor still refuses", []store.Work{work("W-1", "outro@maquina", "E-0001")}, "", nil, []string{"W-1"}, nil},
		{"a work on the card and a child counts once", []store.Work{{ID: "W-1", State: domain.WorkActive, Actor: "outro@maquina", Refs: []string{"E-0001", "T-0001"}}}, "sess-a", nil, []string{"W-1"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			foreign, ambiguous := classifyAmendWorks(tc.works, features, "E-0001", mine, tc.session, sessionsOf(tc.works, tc.bindings), now)
			if got := ids(foreign); !equal(got, tc.wantForeign) {
				t.Fatalf("foreign = %v, want %v", got, tc.wantForeign)
			}
			if got := ids(ambiguous); !equal(got, tc.wantAmbiguous) {
				t.Fatalf("ambiguous = %v, want %v", got, tc.wantAmbiguous)
			}
		})
	}
}

// seedEpicWithBoundChild seeds E-0001 in state with a spec and a plan, a live
// child T-0001 whose active work W-0001 is bound to another session on this
// machine, and runs as session "sess-a" — the case of 2026-11-20.
func seedEpicWithBoundChild(t *testing.T, state domain.FeatureState) (repoDir, projDir, origin string) {
	t.Helper()
	repoDir, projDir, origin = newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "E-0001", state, "spec.md", "plan.md")
	if err := store.SaveFeature(repoDir, store.Feature{ID: "T-0001", Title: "Tarefa", Parent: "E-0001", State: domain.StateInDevelopment}); err != nil {
		t.Fatal(err)
	}
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "T-0001", false)
	if err := BindSession(filepath.Dir(repoDir), "sess-b", "W-0001"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_SESSION", "sess-a")
	return repoDir, projDir, origin
}

// TestAmendRefusesAnotherSessionsCard (criteria 1 and 2): the refusal writes
// nothing and says who is on the card, what the amend would cause in that
// state, and what was written against the current text.
func TestAmendRefusesAnotherSessionsCard(t *testing.T) {
	for _, tc := range []struct {
		state       domain.FeatureState
		consequence string
	}{
		{domain.StateApproved, "stales the approval"},
		{domain.StateInDevelopment, "takes effect at once, with no re-approval"},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			repoDir, projDir, origin := seedEpicWithBoundChild(t, tc.state)
			head0, _ := gitback.HeadSHA(origin)
			evs0, _ := store.ReadEvents(repoDir)

			_, _, _, _, err := AmendFeatureDoc(repoDir, projDir, "E-0001", "spec", amendedSpec, "mudou", false)
			if !errors.Is(err, ErrActiveWorkElsewhere) {
				t.Fatalf("err = %v, want ErrActiveWorkElsewhere", err)
			}
			for _, want := range []string{"W-0001", "on T-0001", autoWorkActor(), "session: sess-b", "signal", "Atividade W-0001", tc.consequence, "plan.md", "T-0001 (in_development)", "--force"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not say %q:\n%v", want, err)
				}
			}
			if head, _ := gitback.HeadSHA(origin); head != head0 {
				t.Fatalf("origin moved: a refused amend commits nothing")
			}
			if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evs0) {
				t.Fatalf("events %d → %d: a refused amend appends nothing", len(evs0), len(evs))
			}
			if _, err := os.Stat(filepath.Join(repoDir, "content", "revisions", "E-0001")); !os.IsNotExist(err) {
				t.Fatalf("a refused amend archives no revision: %v", err)
			}
		})
	}
}

// TestAmendForceRecordsCrossedWorks (criterion 3): --force writes, repeats the
// crossed works and marks guard: forced with their ids on the event.
func TestAmendForceRecordsCrossedWorks(t *testing.T) {
	repoDir, projDir, _ := seedEpicWithBoundChild(t, domain.StateInDevelopment)

	_, rev, changed, notes, err := AmendFeatureDoc(repoDir, projDir, "E-0001", "spec", amendedSpec, "mudou", true)
	if err != nil || !changed || rev != 1 {
		t.Fatalf("forced amend: rev %d, changed %v, err %v", rev, changed, err)
	}
	out := strings.Join(notes, "\n")
	if !strings.Contains(out, "W-0001") || !strings.Contains(out, "session: sess-b") {
		t.Fatalf("notes must repeat the crossed work:\n%s", out)
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.Type != "feature.doc_edited" || last.Payload["guard"] != "forced" || last.Payload["works"] != "W-0001" {
		t.Fatalf("last event = %+v, want doc_edited with guard=forced works=W-0001", last)
	}
}

// TestAmendInDevelopmentSaysItTakesEffect (criteria 5 and 6): with only a same
// actor work and no binding, the amend writes with one warning per work, and
// in development the output says it already holds and names the plan and the
// live children written against the previous text.
func TestAmendInDevelopmentSaysItTakesEffect(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "E-0001", domain.StateInDevelopment, "spec.md", "plan.md")
	if err := store.SaveFeature(repoDir, store.Feature{ID: "T-0001", Title: "Tarefa", Parent: "E-0001", State: domain.StateApproved}); err != nil {
		t.Fatal(err)
	}
	seedActiveWork(t, repoDir, "W-0001", autoWorkActor(), "E-0001", false)
	t.Setenv("CERNE_SESSION", "sess-a")

	_, _, changed, notes, err := AmendFeatureDoc(repoDir, projDir, "E-0001", "spec", amendedSpec, "mudou", false)
	if err != nil || !changed {
		t.Fatalf("amend: changed %v, err %v", changed, err)
	}
	out := strings.Join(notes, "\n")
	for _, want := range []string{"warning: W-0001", "takes effect at once, with no re-approval", "plan.md", "T-0001 (approved)"} {
		if !strings.Contains(out, want) {
			t.Errorf("notes do not say %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "warning:"); n != 1 {
		t.Errorf("warnings = %d, want one per work:\n%s", n, out)
	}
	evs, _ := store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Payload["guard"] != "" {
		t.Fatalf("an unforced amend carries no guard mark: %+v", last)
	}
}

// TestAmendWithoutSessionFallsToActor (criterion 7, end to end; critic C3):
// with no CERNE_SESSION, another session's binding proves nothing, so the same
// actor's work only warns — saying why — and another actor's still refuses.
func TestAmendWithoutSessionFallsToActor(t *testing.T) {
	repoDir, projDir, _ := seedEpicWithBoundChild(t, domain.StateInDevelopment)
	t.Setenv("CERNE_SESSION", "")

	_, _, changed, notes, err := AmendFeatureDoc(repoDir, projDir, "E-0001", "spec", amendedSpec, "mudou", false)
	if err != nil || !changed {
		t.Fatalf("same actor without CERNE_SESSION: changed %v, err %v", changed, err)
	}
	if out := strings.Join(notes, "\n"); !strings.Contains(out, "warning: W-0001 is active on T-0001") || !strings.Contains(out, "no session identity (CERNE_SESSION or CURSOR_CONVERSATION_ID)") {
		t.Fatalf("notes must warn on W-0001 and say sessions were not compared:\n%s", out)
	}

	seedActiveWork(t, repoDir, "W-0002", "outro@maquina", "E-0001", false)
	if _, _, _, _, err := AmendFeatureDoc(repoDir, projDir, "E-0001", "spec", amendedSpec+"\nMais.\n", "de novo", false); !errors.Is(err, ErrActiveWorkElsewhere) || !strings.Contains(err.Error(), "W-0002") {
		t.Fatalf("another actor without CERNE_SESSION: err = %v, want ErrActiveWorkElsewhere naming W-0002", err)
	}
}
