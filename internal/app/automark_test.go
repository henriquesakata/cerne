package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// asActor clears every agent marker of the environment (N-0058) and, when
// actor is not empty, declares it through CERNE_ACTOR.
func asActor(t *testing.T, actor string) {
	t.Helper()
	for _, k := range []string{"CERNE_ACTOR", "CLAUDECODE", "CURSOR_AGENT", "COPILOT_CLI", "CODEX_THREAD_ID"} {
		t.Setenv(k, "")
	}
	if actor != "" {
		t.Setenv("CERNE_ACTOR", actor)
	}
}

func saveGates(t *testing.T, projDir string, g project.GatesConfig) {
	t.Helper()
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: "b", Gates: g}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// lastEntityEvent is the last event of entity id of type typ.
func lastEntityEvent(t *testing.T, repoDir, id, typ string) domain.Event {
	t.Helper()
	evs := entityEvents(t, repoDir, id, typ)
	if len(evs) == 0 {
		t.Fatalf("%s has no %s event", id, typ)
	}
	return evs[len(evs)-1]
}

// TestApproveAutoMarksEntity: only an agent crossing the approve gate set to
// auto marks the card and its event; a person approving under auto leaves
// both unmarked, and a person's re-approval clears an earlier mark.
// F-0515 crit. 6
func TestApproveAutoMarksEntity(t *testing.T) {
	for _, tc := range []struct {
		name, actor, gate, wantVia string
	}{
		{"agent under auto", "agent", project.GateAuto, "auto"},
		{"prefixed agent under auto", "agent:m-1", project.GateAuto, "auto"},
		{"person under auto", "", project.GateAuto, ""},
		{"agent under human (the ui core)", "agent", project.GateHuman, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asActor(t, tc.actor)
			repoDir, projDir, _ := newFeatureBackend(t)
			saveGates(t, projDir, project.GatesConfig{Approve: tc.gate})
			seedCandidateSpec(t, repoDir, "F-0001", approvedSpecA)
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
				t.Fatalf("approve: %v", err)
			}
			if got := reload(t, repoDir, "F-0001").ApprovedVia; got != tc.wantVia {
				t.Errorf("approved_via = %q, want %q", got, tc.wantVia)
			}
			if got := lastEntityEvent(t, repoDir, "F-0001", "feature.approved").Payload["via"]; got != tc.wantVia {
				t.Errorf("approve event via = %q, want %q", got, tc.wantVia)
			}
		})
	}
	t.Run("a person's re-approval clears the mark", func(t *testing.T) {
		asActor(t, "agent")
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGates(t, projDir, project.GatesConfig{Approve: project.GateAuto})
		seedCandidateSpec(t, repoDir, "F-0001", approvedSpecA)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatalf("agent approve: %v", err)
		}
		if got := reload(t, repoDir, "F-0001").ApprovedVia; got != "auto" {
			t.Fatalf("approved_via after the agent = %q, want auto", got)
		}
		if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nCritério novo.\n"); err != nil {
			t.Fatalf("edit: %v", err)
		}
		asActor(t, "")
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatalf("re-approve: %v", err)
		}
		if got := reload(t, repoDir, "F-0001").ApprovedVia; got != "" {
			t.Errorf("approved_via after a person's re-approval = %q, want empty", got)
		}
	})
	t.Run("a revert of the approval clears the mark", func(t *testing.T) {
		asActor(t, "agent")
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGates(t, projDir, project.GatesConfig{Approve: project.GateAuto})
		seedCandidateSpec(t, repoDir, "F-0001", approvedSpecA)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatalf("agent approve: %v", err)
		}
		asActor(t, "")
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionRevert, "", "spec needs another look", nil); err != nil {
			t.Fatalf("revert: %v", err)
		}
		if f := reload(t, repoDir, "F-0001"); f.State != domain.StateCandidate || f.ApprovedVia != "" {
			t.Errorf("after the revert = %s, approved_via %q; want candidate, empty", f.State, f.ApprovedVia)
		}
	})
	t.Run("children an agent's approve carries are marked", func(t *testing.T) {
		asActor(t, "agent")
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGates(t, projDir, project.GatesConfig{Approve: project.GateAuto})
		seedEpicWithChildren(t, repoDir)
		if _, err := TransitionFeature(repoDir, projDir, "E-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatalf("approve E-0001: %v", err)
		}
		if c := reload(t, repoDir, "F-0002"); c.State != domain.StateApproved || c.ApprovedVia != "auto" {
			t.Errorf("F-0002 = %s, approved_via %q; want approved, auto", c.State, c.ApprovedVia)
		}
		if ev := lastEntityEvent(t, repoDir, "F-0002", "feature.approved"); ev.Payload["via"] != "auto" {
			t.Errorf("F-0002 approve event = %+v, want via auto", ev.Payload)
		}
	})
}

// TestCompleteAutoMarksAccepted: a complete the accept gate set to auto
// carries to done marks the card accepted_via auto, besides the event; under
// the human gate the card stops at implemented, unmarked.
// F-0515 crit. 7
func TestCompleteAutoMarksAccepted(t *testing.T) {
	for _, tc := range []struct {
		gate    string
		state   domain.FeatureState
		wantVia string
	}{
		{project.GateAuto, domain.StateDone, "auto"},
		{project.GateHuman, domain.StateImplemented, ""},
	} {
		t.Run(tc.gate, func(t *testing.T) {
			asActor(t, "agent")
			repoDir, projDir, _ := newFeatureBackend(t)
			saveGates(t, projDir, project.GatesConfig{Accept: tc.gate})
			seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if f := reload(t, repoDir, "F-0001"); f.State != tc.state || f.AcceptedVia != tc.wantVia {
				t.Errorf("F-0001 = %s, accepted_via %q; want %s, %q", f.State, f.AcceptedVia, tc.state, tc.wantVia)
			}
		})
	}
}

// TestCompleteAutoMarksCascadedTasks: the implemented tasks a complete under
// accept auto carries to done along with their parent are marked too, on the
// entity and on their accept event.
// F-0515 crit. 7
func TestCompleteAutoMarksCascadedTasks(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateAuto})
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "F-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if task := reload(t, repoDir, "T-0002"); task.State != domain.StateDone || task.AcceptedVia != "auto" {
		t.Fatalf("T-0002 = %s, accepted_via %q; want done, auto", task.State, task.AcceptedVia)
	}
	if ev := lastEntityEvent(t, repoDir, "T-0002", "feature.accepted"); ev.Payload["via"] != "auto" || ev.Payload["cascade_from"] != "F-0001" {
		t.Errorf("T-0002 accept event = %+v, want via auto and cascade_from F-0001", ev.Payload)
	}
	if lines := strings.Join(out.Cascade, "\n"); !strings.Contains(lines, "T-0002: implemented → done along with F-0001") {
		t.Errorf("cascade lines = %q, want T-0002 carried", lines)
	}
}

// TestAcceptAutoMarksAgentAct: an agent's own accept under the accept gate
// set to auto marks the card, its event and the tasks it carries; a person's
// accept, under any gate, marks nothing.
// F-0515 crit. 7
func TestAcceptAutoMarksAgentAct(t *testing.T) {
	for _, tc := range []struct {
		name, actor, gate, wantVia string
		ids                        []string // the cards that reach done
	}{
		{"agent under auto", "agent", project.GateAuto, "auto", []string{"F-0001", "T-0002"}},
		{"person under auto", "", project.GateAuto, "", []string{"F-0001", "T-0002"}},
		// The human gate keeps the agent's tasks (T-0007 crit. 2).
		{"agent under human (the ui core)", "agent", project.GateHuman, "", []string{"F-0001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asActor(t, tc.actor)
			repoDir, projDir, _ := newFeatureBackend(t)
			saveGates(t, projDir, project.GatesConfig{Accept: tc.gate})
			seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented)
			seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "F-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "", nil); err != nil {
				t.Fatalf("accept: %v", err)
			}
			for _, id := range tc.ids {
				if f := reload(t, repoDir, id); f.State != domain.StateDone || f.AcceptedVia != tc.wantVia {
					t.Errorf("%s = %s, accepted_via %q; want done, %q", id, f.State, f.AcceptedVia, tc.wantVia)
				}
				if got := lastEntityEvent(t, repoDir, id, "feature.accepted").Payload["via"]; got != tc.wantVia {
					t.Errorf("%s accept event via = %q, want %q", id, got, tc.wantVia)
				}
			}
		})
	}
}

// TestLegacyCardsStayUnmarked: a card stored before the marks existed, whose
// log holds an agent's approve and an auto accept, reads unmarked and a save
// adds no mark: nothing fills the fields from the history.
// F-0515 crit. 10
func TestLegacyCardsStayUnmarked(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	raw := `{"id":"F-0001","title":"Legacy","module":"","state":"done","created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-02T00:00:00Z"}`
	path := filepath.Join(repoDir, "entities", "features", "F-0001.json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []domain.Event{
		domain.NewEvent("F-0001", "feature.approved", nil, "agent"),
		domain.NewEvent("F-0001", "feature.accepted", map[string]string{"via": "auto"}, "agent"),
	} {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed legacy"); err != nil {
		t.Fatal(err)
	}
	f := reload(t, repoDir, "F-0001")
	if f.ApprovedVia != "" || f.AcceptedVia != "" {
		t.Fatalf("legacy card = approved_via %q, accepted_via %q; want both empty", f.ApprovedVia, f.AcceptedVia)
	}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	if saved, _ := os.ReadFile(path); strings.Contains(string(saved), "approved_via") || strings.Contains(string(saved), "accepted_via") {
		t.Errorf("a save of the legacy card wrote a mark:\n%s", saved)
	}
}
