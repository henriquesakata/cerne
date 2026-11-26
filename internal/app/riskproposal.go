package app

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// The proposals are derived from the log, never stored (F-0518): the
// decisions, the owner's answers, the fixes and the reverts say which are
// due, and an owner's decision on one is an event on each card that
// motivated it, so a dismissed proposal does not reopen from those cards.
const proposalEvent = "feature.gate_proposal"

// riskSignal is one later signal on a card: a fix or a revert, with its
// position in the log.
type riskSignal struct {
	idx  int
	fix  bool // a fix; a revert otherwise
	line string
}

// riskSignals maps each card to its fixes (--fixes, at creation or by set)
// and reverts, in log order.
func riskSignals(evs []domain.Event) map[string][]riskSignal {
	out := map[string][]riskSignal{}
	for i, ev := range evs {
		switch {
		case (ev.Type == "feature.created" || ev.Type == "feature.set") && ev.Payload["fixes"] != "":
			x := ev.Payload["fixes"]
			out[x] = append(out[x], riskSignal{i, true, ev.EntityID + " fixes " + x})
		case ev.Type == eventTypes[domain.ActionRevert]:
			out[ev.EntityID] = append(out[ev.EntityID], riskSignal{i, false, ev.EntityID + " was reverted"})
		}
	}
	return out
}

// gateProposals are the proposals the log calls for against the lists of g.
func gateProposals(repoDir string, g project.GatesConfig, evs []domain.Event, features []store.Feature, decisions map[string]*RiskDecision, signals map[string][]riskSignal) []GateProposal {
	decided := map[string]map[string]bool{} // proposal → cards the owner decided it for
	lows := map[string][]*RiskDecision{}    // card → every accept as low risk
	for i, ev := range evs {
		switch {
		case ev.Type == proposalEvent:
			id := ev.Payload["proposal"]
			if decided[id] == nil {
				decided[id] = map[string]bool{}
			}
			decided[id][ev.EntityID] = true
		case isRiskDecision(ev) && ev.Payload["risk"] == "low":
			lows[ev.EntityID] = append(lows[ev.EntityID], newRiskDecision(ev, i))
		}
	}
	out := []GateProposal{}
	for _, l := range []struct {
		setting string
		items   []string
		facet   bool
	}{{"gates.risk_human_paths", g.RiskHumanPaths(), false}, {"gates.risk_human_facets", g.RiskHumanFacets(), true}} {
		for _, item := range l.items {
			if p, ok := relaxProposal(repoDir, l.setting, l.items, item, l.facet, g.RiskRelaxAfter(), features, decisions, signals, decided); ok {
				out = append(out, p)
			}
		}
	}
	paths := g.RiskHumanPaths()
	for _, f := range features {
		id := "tighten:" + f.ID
		if decided[id][f.ID] {
			continue
		}
		// The last accept as low risk that a fix or a revert followed.
		for k := len(lows[f.ID]) - 1; k >= 0; k-- {
			low := lows[f.ID][k]
			var why []string
			for _, s := range signals[f.ID] {
				if s.idx > low.idx {
					why = append(why, s.line)
				}
			}
			if len(why) == 0 {
				continue
			}
			if add := uncovered(low.Files, paths); len(add) > 0 {
				out = append(out, newProposal(id, "tighten", "gates.risk_human_paths", append(slices.Clone(paths), add...), add, nil,
					[]string{f.ID}, append([]string{f.ID + " was accepted as low risk"}, why...)))
			}
			break
		}
	}
	return out
}

// relaxProposal proposes to drop item from the list setting when as many
// cards as relaxAfter, held by it, got a not-needed answer after the last
// contrary signal on a card that touched it — a should-have-asked answer, a
// fix or a revert. Silence counts for nothing, and the cards the owner
// already decided this proposal for count no more.
func relaxProposal(repoDir, setting string, list []string, item string, facet bool, relaxAfter int, features []store.Feature,
	decisions map[string]*RiskDecision, signals map[string][]riskSignal, decided map[string]map[string]bool) (GateProposal, bool) {
	id := "relax:" + setting + ":" + item
	same := func(x string) bool { return strings.EqualFold(x, item) }
	touched := func(f store.Feature) bool {
		if facet {
			return slices.ContainsFunc(chainFacets(repoDir, f), same)
		}
		// ponytail: matched from the repository root; a project in a
		// subdirectory with project-relative patterns misses a contrary
		// signal here — record the project prefix on the decision if it bites.
		d := decisions[f.ID]
		return d != nil && len(sensitivePaths(d.Files, []string{item}, "")) > 0
	}
	since := -1
	for _, f := range features {
		if !touched(f) {
			continue
		}
		if d := decisions[f.ID]; d != nil && d.Feedback != nil && d.Feedback.Answer == FeedbackShouldHaveAsked {
			since = max(since, d.Feedback.idx)
		}
		for _, s := range signals[f.ID] {
			since = max(since, s.idx)
		}
	}
	var cards, why []string
	for _, f := range features {
		d := decisions[f.ID]
		if d == nil || d.Risk != "held" || d.Feedback == nil || d.Feedback.Answer != FeedbackNotNeeded || d.Feedback.idx <= since || decided[id][f.ID] {
			continue
		}
		heldBy := slices.Contains(splitList(d.payload["risk_patterns"]), item)
		if facet {
			heldBy = slices.ContainsFunc(d.Facets, same)
		}
		if heldBy {
			cards = append(cards, f.ID)
			why = append(why, f.ID+" was held by "+item+" and did not need the owner")
		}
	}
	if len(cards) < relaxAfter {
		return GateProposal{}, false
	}
	after := slices.DeleteFunc(slices.Clone(list), func(x string) bool { return x == item })
	return newProposal(id, "relax", setting, after, nil, []string{item}, cards, why), true
}

func newProposal(id, kind, setting string, after, add, remove, cards, signals []string) GateProposal {
	js, _ := json.Marshal(after)
	return GateProposal{ID: id, Kind: kind, Setting: setting, Add: add, Remove: remove, After: after, Cards: cards, Signals: signals,
		Command: "cerne settings set " + setting + " '" + strings.ReplaceAll(string(js), "'", `'\''`) + "' --scope project"}
}

// uncovered are the folders of files (dir/**; a root file stands for
// itself) that the list does not cover yet: neither listed nor matching every
// file of the folder.
func uncovered(files, list []string) []string {
	var out []string
	for _, g := range dirGlobs(files) {
		in := slices.DeleteFunc(slices.Clone(files), func(f string) bool {
			d := path.Dir(f)
			return !(d == "." && f == g || d+"/**" == g)
		})
		if !slices.Contains(list, g) && len(sensitivePaths(in, list, "")) < len(in) {
			out = append(out, g)
		}
	}
	return out
}

// DecideGateProposal applies or dismisses the open proposal id (F-0518 crit.
// 8): accepted writes the list it names; both record the decision on each
// card that motivated it, so it does not reopen from them. The caller checks
// the actor: the ui is the owner.
func DecideGateProposal(repoDir, projDir, id, decision string) error {
	if decision != ProposalAccepted && decision != ProposalDismissed {
		return fmt.Errorf("unknown decision %q: want %s or %s", decision, ProposalAccepted, ProposalDismissed)
	}
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		evs, err := store.ReadEvents(repoDir)
		if err != nil {
			return "", err
		}
		features, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return "", err
		}
		ps := gateProposals(repoDir, projectGates(projDir), evs, features, riskDecisions(evs), riskSignals(evs))
		i := slices.IndexFunc(ps, func(p GateProposal) bool { return p.ID == id })
		if i < 0 {
			return "", fmt.Errorf("%w: %s", ErrUnknownProposal, id)
		}
		p := ps[i]
		if decision == ProposalAccepted {
			if err := project.SetSettingValue(projDir, p.Setting, project.ScopeProject, p.After); err != nil {
				return "", err
			}
		}
		for _, card := range p.Cards {
			payload := map[string]string{"proposal": p.ID, "decision": decision, "setting": p.Setting,
				"add": strings.Join(p.Add, ", "), "remove": strings.Join(p.Remove, ", ")}
			if err := store.AppendEvent(repoDir, domain.NewEvent(card, proposalEvent, payload, EventActor())); err != nil {
				return "", err
			}
		}
		return "cerne: gate proposal " + p.ID + " " + decision, nil
	})
}
