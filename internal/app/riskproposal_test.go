package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// logged appends ev to the log and commits.
func logged(t *testing.T, repoDir string, evs ...domain.Event) {
	t.Helper()
	for _, ev := range evs {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "log"); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatal(err)
	}
}

// heldAndAnswered seeds n cards from first, held by the gate on go.mod (or
// on the facet data, with facet set), each answered not-needed by the owner.
func heldAndAnswered(t *testing.T, repoDir, projDir string, first, n int, facet bool) []string {
	t.Helper()
	var ids []string
	for i := first; i < first+n; i++ {
		id := fmt.Sprintf("F-%04d", i)
		p, facets := heldPayload(), []string{"code"}
		if facet {
			p = map[string]string{"risk": "held", "risk_files": "internal/x/x.go", "risk_facets": "data"}
			facets = []string{"data"}
		}
		decided(t, repoDir, id, domain.StateDone, facets, p)
		if _, err := RecordRiskFeedback(repoDir, projDir, id, RiskFeedback{Answer: FeedbackNotNeeded}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func proposals(t *testing.T, repoDir, projDir string) []GateProposal {
	t.Helper()
	v, err := ReadRiskGate(repoDir, projDir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return v.Proposals
}

// TestRelaxProposalAfterRepeats: the gate proposes to drop a pattern or a
// facet only after gates.risk_relax_after cards it held get a not-needed
// answer, with no contrary signal since; a held card nobody answered counts
// for nothing.
// F-0518 crit. 6
func TestRelaxProposalAfterRepeats(t *testing.T) {
	for _, c := range []struct {
		name     string
		answered int
		facet    bool
		limit    int64 // gates.risk_relax_after; 0 = unset
		contrary bool  // a fix of the first card after the answers
		want     string
	}{
		{"two answered, one silent", 2, false, 0, false, ""},
		{"three", 3, false, 0, false, "relax:gates.risk_human_paths:go.mod"},
		{"three on a facet", 3, true, 0, false, "relax:gates.risk_human_facets:data"},
		{"three, then a fix", 3, false, 0, true, ""},
		{"three under a limit of five", 3, false, 5, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			asActor(t, "")
			repoDir, projDir, _ := newFeatureBackend(t)
			g := calibration(0)
			if c.limit > 0 {
				g.RiskRelaxAfterN = &c.limit
			}
			saveGates(t, projDir, g)
			ids := heldAndAnswered(t, repoDir, projDir, 1, c.answered, c.facet)
			decided(t, repoDir, "F-0050", domain.StateDone, []string{"data"}, heldPayload()) // held, never answered
			if c.contrary {
				logged(t, repoDir, domain.NewEvent("F-0060", "feature.created", map[string]string{"fixes": ids[0]}, "human"))
			}
			ps := proposals(t, repoDir, projDir)
			if c.want == "" {
				if len(ps) != 0 {
					t.Fatalf("proposals = %+v, want none", ps)
				}
				return
			}
			if len(ps) != 1 || ps[0].ID != c.want || ps[0].Kind != "relax" || !slices.Equal(ps[0].Cards, ids) {
				t.Fatalf("proposals = %+v, want %s motivated by %v", ps, c.want, ids)
			}
			p := ps[0]
			if slices.Contains(p.After, p.Remove[0]) || len(p.After) == 0 || !strings.HasPrefix(p.Command, "cerne settings set "+p.Setting+" '") {
				t.Errorf("proposal %+v: want the list without %v and the exact settings set", p, p.Remove)
			}
		})
	}
}

// TestRelaxSeesContraryInASubdirectory: in a project under a subdirectory, a
// project-relative pattern the gate recorded as hit counts a contrary signal
// on that card too, so a fix after three not-needed answers keeps the drop
// from being proposed.
// F-0518 crit. 6
func TestRelaxSeesContraryInASubdirectory(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	g := calibration(0)
	g.RiskPaths = []string{"src/**"}
	saveGates(t, projDir, g)
	for _, id := range []string{"F-0001", "F-0002", "F-0003"} {
		decided(t, repoDir, id, domain.StateDone, []string{"code"}, map[string]string{"risk": "held", "risk_files": "app/src/x.go", "risk_paths": "app/src/x.go", "risk_patterns": "src/**"})
		if _, err := RecordRiskFeedback(repoDir, projDir, id, RiskFeedback{Answer: FeedbackNotNeeded}); err != nil {
			t.Fatal(err)
		}
	}
	if ps := proposals(t, repoDir, projDir); len(ps) != 1 {
		t.Fatalf("proposals before the fix = %+v, want the drop of src/**", ps)
	}
	logged(t, repoDir, domain.NewEvent("F-0009", "feature.created", map[string]string{"fixes": "F-0002"}, "human"))
	if ps := proposals(t, repoDir, projDir); len(ps) != 0 {
		t.Fatalf("proposals after a fix of a card held by src/** = %+v, want none", ps)
	}
}

// TestFixOpensTightenProposal: a fix of a card accepted as low risk opens a
// proposal to add the folders that delivery touched, citing the card and
// the fix.
// F-0518 crit. 7
func TestFixOpensTightenProposal(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, calibration(0))
	p := lowPayload()
	p["risk_files"] = "internal/x/x.go, internal/y/y.go, README.md"
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, p)
	if ps := proposals(t, repoDir, projDir); len(ps) != 0 {
		t.Fatalf("proposals before any fix = %+v", ps)
	}
	logged(t, repoDir, domain.NewEvent("F-0005", "feature.created", map[string]string{"fixes": "F-0001"}, "human"))
	ps := proposals(t, repoDir, projDir)
	if len(ps) != 1 || ps[0].ID != "tighten:F-0001" || ps[0].Setting != "gates.risk_human_paths" || !slices.Equal(ps[0].Add, []string{"README.md", "internal/x/**", "internal/y/**"}) {
		t.Fatalf("proposals = %+v, want tighten:F-0001 adding the folders", ps)
	}
	if !slices.Equal(ps[0].Cards, []string{"F-0001"}) || !strings.Contains(strings.Join(ps[0].Signals, " "), "F-0005 fixes F-0001") {
		t.Errorf("proposal %+v: want it to cite the card and the fix", ps[0])
	}
}

// TestRevertOpensTightenProposal: a revert of a card accepted as low risk
// opens the same proposal, citing the revert.
// F-0518 crit. 7
func TestRevertOpensTightenProposal(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, calibration(0))
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	logged(t, repoDir, domain.NewEvent("F-0001", "feature.reverted", map[string]string{"reason": "broke prod"}, "human"))
	// A re-complete under the gate does not erase the accept that was reverted.
	logged(t, repoDir, domain.NewEvent("F-0001", "feature.completed", heldPayload(), "agent"))
	ps := proposals(t, repoDir, projDir)
	if len(ps) != 1 || ps[0].ID != "tighten:F-0001" || !slices.Equal(ps[0].Add, []string{"internal/x/**"}) || !strings.Contains(strings.Join(ps[0].Signals, " "), "F-0001 was reverted") {
		t.Fatalf("proposals = %+v, want tighten:F-0001 citing the revert", ps)
	}
}

// TestProposalAcceptAndDismiss: accepting a proposal writes the list it
// names and closes it; dismissing one writes nothing to the list and closes
// it; both are recorded on the cards that motivated it. Nothing applies on
// its own.
// F-0518 crit. 8
func TestProposalAcceptAndDismiss(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, calibration(0))
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	logged(t, repoDir, domain.NewEvent("F-0005", "feature.created", map[string]string{"fixes": "F-0001"}, "human"))
	ids := heldAndAnswered(t, repoDir, projDir, 10, 3, true)
	if ps := proposals(t, repoDir, projDir); len(ps) != 2 {
		t.Fatalf("proposals = %+v, want the tighten and the relax", ps)
	}
	if cfg, _, _ := project.LoadConfig(projDir); cfg.Gates.RiskPaths != nil || cfg.Gates.RiskFacets != nil {
		t.Fatalf("a proposal applied itself: %v / %v", cfg.Gates.RiskPaths, cfg.Gates.RiskFacets)
	}
	if err := DecideGateProposal(repoDir, projDir, "tighten:F-0001", ProposalAccepted); err != nil {
		t.Fatal(err)
	}
	if err := DecideGateProposal(repoDir, projDir, "relax:gates.risk_human_facets:data", ProposalDismissed); err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := project.LoadConfig(projDir)
	if !slices.Contains(cfg.Gates.RiskHumanPaths(), "internal/x/**") || !slices.Contains(cfg.Gates.RiskHumanFacets(), "data") {
		t.Errorf("lists %v / %v: want the accepted folder in and the dismissed facet kept", cfg.Gates.RiskHumanPaths(), cfg.Gates.RiskHumanFacets())
	}
	if ps := proposals(t, repoDir, projDir); len(ps) != 0 {
		t.Errorf("proposals after deciding both = %+v, want none", ps)
	}
	if got := lastEntityEvent(t, repoDir, ids[0], "feature.gate_proposal").Payload; got["decision"] != ProposalDismissed || got["proposal"] != "relax:gates.risk_human_facets:data" {
		t.Errorf("dismissal on %s = %v", ids[0], got)
	}
	if err := DecideGateProposal(repoDir, projDir, "tighten:F-0001", ProposalAccepted); !errors.Is(err, ErrUnknownProposal) {
		t.Errorf("deciding a closed proposal: err = %v, want ErrUnknownProposal", err)
	}
}

// TestRiskGateStats: over a period, how many cards the gate accepted alone
// and how many it held, and how many of each got an answer, a fix or a
// revert; a decision before the period is out. The calibration's progress
// separates what the gate would have let pass that the owner said needed
// them.
// F-0518 crit. 10, 13
func TestRiskGateStats(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	calibrating(t, projDir, 4)
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	decided(t, repoDir, "F-0002", domain.StateDone, []string{"code"}, lowPayload())
	decided(t, repoDir, "F-0003", domain.StateDone, []string{"code"}, heldPayload())
	cal := func(would string) map[string]string {
		return map[string]string{"risk": "held", "risk_calibration": "0/4", "risk_would": would, "risk_files": "internal/x/x.go"}
	}
	decided(t, repoDir, "F-0004", domain.StateDone, []string{"code"}, cal("low"))
	decided(t, repoDir, "F-0005", domain.StateDone, []string{"code"}, cal("held"))
	decided(t, repoDir, "F-0006", domain.StateDone, []string{"code"}, cal("low"))
	for id, answer := range map[string]string{"F-0001": FeedbackShouldHaveAsked, "F-0003": FeedbackNotNeeded, "F-0004": FeedbackShouldHaveAsked, "F-0005": FeedbackShouldHaveAsked, "F-0006": FeedbackNotNeeded} {
		if _, err := RecordRiskFeedback(repoDir, projDir, id, RiskFeedback{Answer: answer}); err != nil {
			t.Fatal(err)
		}
	}
	logged(t, repoDir, domain.NewEvent("F-0009", "feature.created", map[string]string{"fixes": "F-0002"}, "human"),
		domain.NewEvent("F-0003", "feature.reverted", nil, "human"))
	v, err := ReadRiskGate(repoDir, projDir, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if want := (RiskGroup{Cards: 2, Feedback: 1, Fixed: 1}); v.Stats.Accepted != want {
		t.Errorf("accepted = %+v, want %+v", v.Stats.Accepted, want)
	}
	if want := (RiskGroup{Cards: 4, Feedback: 4, Reverted: 1}); v.Stats.Held != want {
		t.Errorf("held = %+v, want %+v", v.Stats.Held, want)
	}
	if want := (RiskCalibration{Target: 4, Answers: 5, Remaining: 0, Answered: 3, Matched: 2, Missed: 1}); v.Calibration != want {
		t.Errorf("calibration = %+v, want %+v", v.Calibration, want)
	}
	if v, _ := ReadRiskGate(repoDir, projDir, time.Now().Add(time.Hour)); v.Stats.Accepted.Cards+v.Stats.Held.Cards != 0 {
		t.Errorf("a period after every decision counts %+v", v.Stats)
	}
}

// TestDismissedProposalStaysClosed: a dismissed proposal does not reopen from
// the cards that motivated it — not when one more card adds to them, and not
// when the same card is fixed again.
// F-0518 crit. 8
func TestDismissedProposalStaysClosed(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, calibration(0))
	heldAndAnswered(t, repoDir, projDir, 1, 3, false)
	decided(t, repoDir, "F-0020", domain.StateDone, []string{"code"}, lowPayload())
	logged(t, repoDir, domain.NewEvent("F-0021", "feature.created", map[string]string{"fixes": "F-0020"}, "human"))
	for _, id := range []string{"relax:gates.risk_human_paths:go.mod", "tighten:F-0020"} {
		if err := DecideGateProposal(repoDir, projDir, id, ProposalDismissed); err != nil {
			t.Fatal(err)
		}
	}
	heldAndAnswered(t, repoDir, projDir, 4, 1, false)
	logged(t, repoDir, domain.NewEvent("F-0022", "feature.created", map[string]string{"fixes": "F-0020"}, "human"))
	if ps := proposals(t, repoDir, projDir); len(ps) != 0 {
		t.Fatalf("proposals = %+v, want the dismissed ones closed", ps)
	}
	// Three new cards are a new proposal, from those cards only.
	heldAndAnswered(t, repoDir, projDir, 5, 2, false)
	ps := proposals(t, repoDir, projDir)
	if len(ps) != 1 || !slices.Equal(ps[0].Cards, []string{"F-0004", "F-0005", "F-0006"}) {
		t.Fatalf("proposals = %+v, want go.mod again from F-0004..F-0006", ps)
	}
}
