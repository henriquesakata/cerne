package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedFeatureInState persists a feature directly in the given state, with the
// lifecycle docs it would have accumulated, and commits the seed so the next
// transition's auto-rebase finds a clean clone.
// tagFeature gives feature id a component, so its approve readiness carries
// no untagged impediment (F-0336).
func tagFeature(t *testing.T, repoDir, id string) {
	t.Helper()
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	f.Components = []string{"cli"}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
}

func seedFeatureInState(t *testing.T, repoDir string, id string, state domain.FeatureState, docs ...string) {
	t.Helper()
	f := store.Feature{ID: id, Title: "Feature " + id, State: state}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	for _, doc := range docs {
		path := FeatureDocPath(repoDir, id, doc)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		body := "# Doc\n\n## Conteúdo real\n\nCorpo real do documento, não um stub.\n" + testBarTail
		if doc == "result.md" {
			body += testGauntletTail // the complete gate charges the record (F-0303)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", doc, err)
		}
	}
	// The transition's critical section rebases onto origin: the seed must be
	// committed and pushed or it is swept before the transition reads it.
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
}

// TestAcceptFeature (F-0131 crit. 1, 3): implemented + accept → done, with the
// feature.accepted event carrying the optional reason.
func TestAcceptFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "validado em homologação", nil)
	if err != nil {
		t.Fatalf("TransitionFeature accept: %v", err)
	}
	if out.From != domain.StateImplemented || out.To != domain.StateDone {
		t.Fatalf("outcome = %+v, want implemented → done", out)
	}
	got, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil || got.State != domain.StateDone {
		t.Fatalf("LoadFeature = %+v, %v; want done persisted", got, err)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "feature.accepted" || last.EntityID != "F-0001" || last.Payload["reason"] != "validado em homologação" {
		t.Fatalf("last event = %+v, want feature.accepted with reason", last)
	}
}

// TestAcceptRefusesWrongState (crit. 2): accept from any non-implemented state
// is an ErrInvalidTransition.
func TestAcceptRefusesWrongState(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment)

	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "", nil)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

// TestCheckHumanGate (crit. 9, 11): under a human gate an agent actor is
// refused; a human (no CERNE_ACTOR) passes; an auto gate lets the agent in.
func TestCheckHumanGate(t *testing.T) {
	cases := []struct {
		name    string
		gate    string
		actor   string
		wantErr bool
	}{
		{"human gate blocks agent", project.GateHuman, "agent", true},
		{"human gate passes human", project.GateHuman, "", false},
		{"human gate passes other actor", project.GateHuman, "local", false},
		{"auto gate passes agent", project.GateAuto, "agent", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", c.actor)
			err := CheckHumanGate(c.gate)
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if c.wantErr && !errors.Is(err, ErrHumanGate) {
				t.Fatalf("err = %v, want errors.Is ErrHumanGate", err)
			}
		})
	}
}

// TestCompleteAutoPromotesToDone (crit. 10): with [gates] accept = "auto" the
// complete transition carries the feature straight to done, in one commit,
// with both lifecycle events appended.
func TestCompleteAutoPromotesToDone(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	cfg := project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Accept: project.GateAuto}}
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("TransitionFeature complete: %v", err)
	}
	if out.To != domain.StateDone {
		t.Fatalf("outcome To = %s, want done (auto-accept)", out.To)
	}
	got, _ := store.LoadFeature(repoDir, "F-0001")
	if got.State != domain.StateDone {
		t.Fatalf("state = %s, want done", got.State)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var completed, accepted bool
	for _, ev := range evs {
		if ev.EntityID != "F-0001" {
			continue
		}
		switch ev.Type {
		case "feature.completed":
			completed = true
		case "feature.accepted":
			accepted = true
			if ev.Payload["via"] != "auto" {
				t.Fatalf("accepted payload = %+v, want via=auto", ev.Payload)
			}
		}
	}
	if !completed || !accepted {
		t.Fatalf("events completed=%v accepted=%v, want both", completed, accepted)
	}
}

// TestCompleteHumanGateStopsAtImplemented (crit. 10, default): without the
// bypass, complete lands on implemented ("Ready") and waits for the human.
func TestCompleteHumanGateStopsAtImplemented(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("TransitionFeature complete: %v", err)
	}
	if out.To != domain.StateImplemented {
		t.Fatalf("outcome To = %s, want implemented (human gate keeps Ready)", out.To)
	}
}
