package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// decided seeds feature id in state with a feature.completed event carrying
// the risk gate's decision payload (as F-0516 records it) and commits.
func decided(t *testing.T, repoDir, id string, state domain.FeatureState, facets []string, payload map[string]string) {
	t.Helper()
	if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Card " + id, State: state, Facets: facets}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent(id, "feature.completed", payload, "agent")); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatal(err)
	}
}

// lowPayload and heldPayload are a decision of the risk gate: accepted as
// low risk, and held on go.mod.
func lowPayload() map[string]string {
	return map[string]string{"risk": "low", "risk_area": "touches no sensitive path or facet", "risk_verdict": "win", "risk_criteria_tested": "2/2",
		"risk_files": "internal/x/x.go, internal/x/x_test.go", "risk_paths": "", "risk_facets": "", "risk_base": "b1", "risk_head": "h1"}
}

func heldPayload() map[string]string {
	return map[string]string{"risk": "held", "risk_area": "touches a sensitive path: go.mod", "risk_verdict": "win", "risk_criteria_tested": "2/2",
		"risk_files": "go.mod, internal/x/x.go", "risk_paths": "go.mod", "risk_patterns": "go.mod", "risk_facets": "", "risk_base": "b2", "risk_head": "h2"}
}

// TestFeedbackEventPayload: the feedback is an event of the card with the
// answer, the reason and the measurements the gate recorded on its decision.
// F-0518 crit. 4
func TestFeedbackEventPayload(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	if _, err := RecordRiskFeedback(repoDir, projDir, "F-0001", RiskFeedback{Answer: FeedbackShouldHaveAsked, Reason: "touched the parser"}); err != nil {
		t.Fatal(err)
	}
	p := lastEntityEvent(t, repoDir, "F-0001", riskFeedbackEvent).Payload
	for k, want := range map[string]string{"answer": FeedbackShouldHaveAsked, "reason": "touched the parser", "risk": "low", "risk_verdict": "win", "risk_criteria_tested": "2/2", "risk_head": "h1", "risk_files": "internal/x/x.go, internal/x/x_test.go"} {
		if p[k] != want {
			t.Errorf("payload[%s] = %q, want %q (payload %v)", k, p[k], want, p)
		}
	}
}

// TestFeedbackReplacesPrevious: a second answer on the same card is the one
// that counts; the first stays in the log.
// F-0518 crit. 4
func TestFeedbackReplacesPrevious(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, heldPayload())
	for _, reason := range []string{"first", "second"} {
		if _, err := RecordRiskFeedback(repoDir, projDir, "F-0001", RiskFeedback{Answer: FeedbackNotNeeded, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	if evs := entityEvents(t, repoDir, "F-0001", riskFeedbackEvent); len(evs) != 2 {
		t.Fatalf("%d feedback events, want both kept in the log", len(evs))
	}
	d, err := RiskDecisionOf(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.Feedback == nil || d.Feedback.Reason != "second" {
		t.Fatalf("decision %+v: want the second answer as the one that counts", d)
	}
}

// TestFeedbackMatchesTheDecision: a card the gate never decided, and an
// answer that does not match what the gate decided, are refused with a
// sentinel that names the decision; nothing is written.
// F-0518 crit. 2
func TestFeedbackMatchesTheDecision(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	decided(t, repoDir, "F-0002", domain.StateDone, []string{"code"}, heldPayload())
	decided(t, repoDir, "F-0003", domain.StateDone, []string{"code"}, map[string]string{"by": "x"})
	for _, c := range []struct {
		id, answer string
		want       error
		says       string
	}{
		{"F-0001", FeedbackNotNeeded, ErrFeedbackMismatch, "accepted as low risk"},
		{"F-0002", FeedbackShouldHaveAsked, ErrFeedbackMismatch, "held"},
		{"F-0003", FeedbackNotNeeded, ErrNoRiskDecision, "risk gate"},
	} {
		_, err := RecordRiskFeedback(repoDir, projDir, c.id, RiskFeedback{Answer: c.answer})
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s %s: err = %v, want %v naming %q", c.id, c.answer, err, c.want, c.says)
		}
		if evs := entityEvents(t, repoDir, c.id, riskFeedbackEvent); len(evs) != 0 {
			t.Errorf("%s: a refused answer wrote %d events", c.id, len(evs))
		}
	}
}
