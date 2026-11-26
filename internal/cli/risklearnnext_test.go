package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// calibratedProject is a project under the risk gate, in calibration, whose
// backend holds F-0001 and F-0002 held in calibration and accepted on the
// merge; F-0002 is answered.
func calibratedProject(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	setAcceptGate(t, projDir, project.GateRisk)
	for _, id := range []string{"F-0001", "F-0002"} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Card " + id, State: domain.StateDone, AcceptedVia: "merge", Facets: []string{"code"}}); err != nil {
			t.Fatal(err)
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "feature.completed", map[string]string{"risk": "held", "risk_calibration": "0/20", "risk_would": "low"}, "agent")); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent("F-0002", "feature.risk_feedback", map[string]string{"answer": "not_needed"}, "human")); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	return projDir, repoDir
}

// proposalProject is a project under the risk gate, past calibration, whose
// backend holds F-0001 accepted as low risk and then fixed by F-0005: the
// gate proposes to add internal/x/** to its paths.
func proposalProject(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	setAcceptGate(t, projDir, project.GateRisk)
	endCalibration(t, projDir)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Card F-0001", State: domain.StateDone, AcceptedVia: "risk", Facets: []string{"code"}}); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []domain.Event{
		domain.NewEvent("F-0001", "feature.completed", map[string]string{"risk": "low", "risk_files": "internal/x/x.go"}, "agent"),
		domain.NewEvent("F-0005", "feature.created", map[string]string{"fixes": "F-0001"}, "human"),
	} {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	return projDir, repoDir
}

// TestNextListsGateProposals: next lists each open proposal in GATE
// PROPOSALS — what changes, in which list, the cards behind it — with the
// exact cerne settings set that applies it; that command, run by the owner,
// applies it and closes it.
// F-0518 crit. 8
func TestNextListsGateProposals(t *testing.T) {
	projDir, _ := proposalProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	for _, want := range []string{"GATE PROPOSALS (1)", "add internal/x/** to gates.risk_human_paths", "F-0005 fixes F-0001", "cerne settings set gates.risk_human_paths '[", `"internal/x/**"]' --scope project`} {
		if !strings.Contains(out, want) {
			t.Errorf("next lacks %q:\n%s", want, out)
		}
	}
	js, err := execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var q struct {
		GateProposals []struct {
			ID, Command string
		} `json:"gate_proposals"`
	}
	if err := json.Unmarshal([]byte(js), &q); err != nil || len(q.GateProposals) != 1 || q.GateProposals[0].ID != "tighten:F-0001" {
		t.Fatalf("next --json gate_proposals = %+v (%v)", q.GateProposals, err)
	}
	cmd := q.GateProposals[0].Command
	list := cmd[strings.Index(cmd, "'")+1 : strings.LastIndex(cmd, "'")]
	if out, err := execCerne(t, "settings", "set", "gates.risk_human_paths", list, "--scope", "project", "--dir", projDir); err != nil {
		t.Fatalf("the printed command: %v\n%s", err, out)
	}
	if out, _ := execCerne(t, "next", "--dir", projDir); strings.Contains(out, "GATE PROPOSALS") {
		t.Errorf("the applied proposal is still listed:\n%s", out)
	}
}

// TestProposalRefusesAgent: applying a proposal is the owner's write; an
// agent that runs the command next prints is refused with ErrHumanGate.
// F-0518 crit. 9
func TestProposalRefusesAgent(t *testing.T) {
	projDir, _ := proposalProject(t)
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "settings", "set", "gates.risk_human_paths", `["internal/x/**"]`, "--scope", "project", "--dir", projDir)
	if !errors.Is(err, app.ErrHumanGate) {
		t.Fatalf("an agent applying a proposal: err = %v (%s), want ErrHumanGate", err, out)
	}
}

// TestNextStopsAskingAfterCalibration: once the number of answers is in, or
// calibration is off, next asks nothing and says nothing about holding
// every delivery.
// F-0518 crit. 12
func TestNextStopsAskingAfterCalibration(t *testing.T) {
	for _, n := range []string{"1", "0"} {
		projDir, _ := calibratedProject(t) // F-0002 answered: one answer
		if out, err := execCerne(t, "settings", "set", "gates.risk_calibration", n, "--scope", "project", "--dir", projDir); err != nil {
			t.Fatalf("set calibration %s: %v\n%s", n, err, out)
		}
		out, err := execCerne(t, "next", "--dir", projDir)
		if err != nil {
			t.Fatalf("next: %v\n%s", err, out)
		}
		if strings.Contains(out, "RISK CALIBRATION") || strings.Contains(out, "holds every delivery") {
			t.Errorf("calibration %s: next still asks:\n%s", n, out)
		}
	}
}

// TestNextAsksTheCalibrationQuestion: in calibration, next asks the owner
// about each card accepted on the merge that has no answer yet, with the
// command of each answer; an answered card is not asked again.
// F-0518 crit. 12
func TestNextAsksTheCalibrationQuestion(t *testing.T) {
	projDir, _ := calibratedProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	for _, want := range []string{"RISK CALIBRATION (1)", "Did this delivery need you?", "1 of 20", "cerne feature feedback F-0001 --should-have-asked", "--not-needed"} {
		if !strings.Contains(out, want) {
			t.Errorf("next lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "feedback F-0002") {
		t.Errorf("next asks about the answered F-0002:\n%s", out)
	}
	js, err := execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var q struct {
		RiskQuestions []string `json:"risk_questions"`
	}
	if err := json.Unmarshal([]byte(js), &q); err != nil || len(q.RiskQuestions) != 1 || q.RiskQuestions[0] != "F-0001" {
		t.Errorf("next --json risk_questions = %v (%v), want [F-0001]", q.RiskQuestions, err)
	}
}
