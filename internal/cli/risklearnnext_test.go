package cli

import (
	"encoding/json"
	"strings"
	"testing"

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
