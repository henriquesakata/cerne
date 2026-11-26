package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// decidedProject is a project under the risk gate whose backend holds
// F-0001 accepted as low risk, F-0002 held on go.mod and accepted on the
// merge, and F-0003 accepted with no risk decision.
func decidedProject(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	setAcceptGate(t, projDir, project.GateRisk)
	for _, c := range []struct {
		id, via string
		payload map[string]string
	}{
		{"F-0001", "risk", map[string]string{"risk": "low", "risk_files": "internal/x/x.go", "risk_head": "h1"}},
		{"F-0002", "merge", map[string]string{"risk": "held", "risk_files": "go.mod", "risk_paths": "go.mod", "risk_patterns": "go.mod", "risk_head": "h2"}},
		{"F-0003", "", map[string]string{"by": "x"}},
	} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: c.id, Title: "Card " + c.id, State: domain.StateDone, AcceptedVia: c.via, Facets: []string{"code"}}); err != nil {
			t.Fatal(err)
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(c.id, "feature.completed", c.payload, "agent")); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	return projDir, repoDir
}

// TestFeatureFeedbackCLI: both answers record on the decision they fit; a
// card the gate never decided and an answer that does not fit are refused
// with the sentinel, naming the decision the card had.
// F-0518 crit. 2
func TestFeatureFeedbackCLI(t *testing.T) {
	projDir, repoDir := decidedProject(t)
	for _, c := range []struct {
		name string
		args []string
		want error
		says string
	}{
		{"should have asked on a low-risk accept", []string{"F-0001", "--should-have-asked", "--reason", "touched the parser"}, nil, "accepted as low risk"},
		{"not needed on a hold", []string{"F-0002", "--not-needed"}, nil, "held for a person"},
		{"no decision", []string{"F-0003", "--not-needed"}, app.ErrNoRiskDecision, "risk gate"},
		{"not needed on a low-risk accept", []string{"F-0001", "--not-needed"}, app.ErrFeedbackMismatch, "accepted as low risk"},
		{"should have asked on a hold", []string{"F-0002", "--should-have-asked"}, app.ErrFeedbackMismatch, "held for a person"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := execCerne(t, append([]string{"feature", "feedback"}, append(c.args, "--dir", projDir)...)...)
			if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v\n%s", err, c.want, out)
			}
			msg := out
			if err != nil {
				msg = err.Error()
			}
			if !strings.Contains(msg, c.says) {
				t.Errorf("output %q does not name %q", msg, c.says)
			}
		})
	}
	if got := lastEventOfType(t, repoDir, "F-0001", "feature.risk_feedback").Payload; got["answer"] != app.FeedbackShouldHaveAsked || got["reason"] != "touched the parser" {
		t.Errorf("F-0001 feedback payload = %v", got)
	}
	for _, args := range [][]string{{"F-0001"}, {"F-0001", "--should-have-asked", "--not-needed"}} {
		if _, err := execCerne(t, append([]string{"feature", "feedback"}, append(args, "--dir", projDir)...)...); err == nil {
			t.Errorf("%v: want a refusal (pass exactly one answer)", args)
		}
	}
}

// TestFeatureFeedbackMarksPaths: with --should-have-asked the owner names
// the paths and facets that need a person from now on; they are in the lists
// as soon as the command returns, and the output says so.
// F-0518 crit. 5
func TestFeatureFeedbackMarksPaths(t *testing.T) {
	projDir, _ := decidedProject(t)
	out, err := execCerne(t, "feature", "feedback", "F-0001", "--should-have-asked", "--add-path", "internal/x/**", "--add-facet", "code", "--dir", projDir)
	if err != nil {
		t.Fatalf("feedback: %v\n%s", err, out)
	}
	if !strings.Contains(out, "gates.risk_human_paths: internal/x/**") || !strings.Contains(out, "gates.risk_human_facets: code") {
		t.Errorf("output does not say what entered the lists:\n%s", out)
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Gates.RiskHumanPaths(), "internal/x/**") || !slices.Contains(cfg.Gates.RiskHumanFacets(), "code") {
		t.Errorf("lists = %v / %v, want the marks in", cfg.Gates.RiskHumanPaths(), cfg.Gates.RiskHumanFacets())
	}
}

// TestFeatureFeedbackRefusesAgent: the feedback is the owner's opinion of the
// gate; an agent gets ErrHumanGate and nothing is written.
// F-0518 crit. 3
func TestFeatureFeedbackRefusesAgent(t *testing.T) {
	projDir, repoDir := decidedProject(t)
	t.Setenv("CERNE_ACTOR", "agent")
	for _, args := range [][]string{{"F-0001", "--should-have-asked"}, {"F-0002", "--not-needed"}} {
		out, err := execCerne(t, append([]string{"feature", "feedback"}, append(args, "--dir", projDir)...)...)
		if !errors.Is(err, app.ErrHumanGate) {
			t.Errorf("%v: err = %v (%s), want ErrHumanGate", args, err, out)
		}
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Type == "feature.risk_feedback" {
			t.Fatalf("an agent's feedback was written: %+v", ev)
		}
	}
}
