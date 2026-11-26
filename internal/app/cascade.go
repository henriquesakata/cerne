package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// cascadeFrom is the state a child leaves when a parent's action carries it.
var cascadeFrom = map[domain.Action]domain.FeatureState{
	domain.ActionApprove: domain.StateCandidate,
	domain.ActionAccept:  domain.StateImplemented,
}

// cascaded is the outcome of a parent's cascade (T-0007): the lines the
// caller prints and the children it moved, for their webhooks.
type cascaded struct {
	lines []string
	moved []store.Feature
}

// cascadeApprove carries a parent's approve to its direct candidate children
// (T-0007 crit. 1), inside the parent's critical section: each child the
// approve check passes (no impediment, the --check exit) is approved like a
// manual approve — the event with its bar and cascade_from, the approved spec
// kept, the plan scaffolded; every other candidate child stays, listed with
// its impediment codes. An actor the approve's human gate refuses takes no
// child along (crit. 2), and the outcome says so: the CLI refuses the parent
// first, and the UI calls the core with no gate of its own. A child an
// agent's act carries under the auto gate is marked like its parent (F-0515).
func cascadeApprove(repoDir, projDir, parentID string, gauntletOn, agentAct bool) (cascaded, error) {
	children, err := childrenOf(repoDir, parentID, func(c store.Feature) bool { return c.State == cascadeFrom[domain.ActionApprove] })
	if err != nil {
		return cascaded{}, err
	}
	if CheckHumanGate(projectGates(projDir).ApproveGate()) != nil {
		return keptByHumanGate(parentID, children), nil
	}
	var out cascaded
	for _, c := range children {
		imps, err := approveReadiness(repoDir, c.ID, gauntletOn)
		if err != nil {
			return out, err
		}
		if len(imps) > 0 {
			var codes []string
			for _, im := range imps {
				codes = append(codes, im.Code)
			}
			out.lines = append(out.lines, fmt.Sprintf("%s stays in candidate: %s — details: 'cerne feature approve %s --check'", c.ID, strings.Join(codes, ", "), c.ID))
			continue
		}
		payload := map[string]string{"cascade_from": parentID}
		if c.ApprovedVia = autoVia(projectGates(projDir).ApproveGate(), agentAct); c.ApprovedVia != "" {
			payload["via"] = c.ApprovedVia // F-0515
		}
		if !gauntletOn {
			payload["gauntlet"] = "off"
		}
		bar, err := AcceptedBar(repoDir, c.ID)
		if err != nil {
			return out, err
		}
		if bar != "" {
			payload["bar"] = bar
			q, _, err := AcceptedJudgeQuestion(repoDir, c.ID)
			if err != nil {
				return out, err
			}
			if q != "" {
				payload["judge_question"] = q
			}
		}
		if err := moveChild(repoDir, &c, domain.ActionApprove, payload, EventActor()); err != nil {
			return out, err
		}
		if err := writePlanStub(repoDir, c.ID, c.Title); err != nil {
			return out, err
		}
		if err := keepApprovedSpec(repoDir, c.ID); err != nil {
			return out, err
		}
		out.moved = append(out.moved, c)
		out.lines = append(out.lines, fmt.Sprintf("%s: %s → %s along with %s", c.ID, cascadeFrom[domain.ActionApprove], c.State, parentID))
	}
	return out, nil
}

// cascadeAccept carries a parent's accept to its implemented tasks (T-0007
// crit. 3), inside the parent's critical section: a task never had a gate of
// its own, so the human who accepts the parent accepts it — its event carries
// cascade_from, and its pendings and ideas follow it like a manual accept.
// An F- child had its own approve and keeps its own accept; an actor the
// accept's human gate refuses takes no task along. An accept on merge (m,
// F-0488) is the human's whoever records it: the tasks ride it, marked alike.
// The parent's accepted_via marks its tasks too: auto (F-0515), and risk
// (F-0516), the gate's own accept, which no human gate stands behind.
func cascadeAccept(repoDir, projDir string, parent store.Feature, m *mergeAccept) (cascaded, error) {
	parentID, via := parent.ID, parent.AcceptedVia
	children, err := childrenOf(repoDir, parentID, func(c store.Feature) bool { return isTask(c.ID) && c.State == cascadeFrom[domain.ActionAccept] })
	if err != nil {
		return cascaded{}, err
	}
	if m == nil && via != project.GateRisk && CheckHumanGate(projectGates(projDir).AcceptGate()) != nil {
		return keptByHumanGate(parentID, children), nil
	}
	var out cascaded
	for _, c := range children {
		payload, actor := map[string]string{"cascade_from": parentID}, EventActor()
		if m != nil {
			payload, actor = m.payload(), mergeActor
			payload["cascade_from"] = parentID
			c.AcceptedVia = "merge"
		}
		if m == nil && (via == project.GateAuto || via == project.GateRisk) {
			payload["via"] = via
			c.AcceptedVia, c.AcceptedRisk = via, parent.AcceptedRisk
		}
		if err := moveChild(repoDir, &c, domain.ActionAccept, payload, actor); err != nil {
			return out, err
		}
		out.moved = append(out.moved, c)
		out.lines = append(out.lines, fmt.Sprintf("%s: %s → %s along with %s", c.ID, cascadeFrom[domain.ActionAccept], c.State, parentID))
		pendings, err := movePaidPendings(repoDir, c, "")
		if err != nil {
			return out, err
		}
		ideas, err := moveLinkedIdeas(repoDir, c, "")
		if err != nil {
			return out, err
		}
		out.lines = append(append(out.lines, pendings...), ideas...)
	}
	return out, nil
}

// childBarOutcome is a child's bar and Gauntlet outcome for the epic's
// closing roster (T-0007 crit. 7): " — <bar line> — Rodada 1: derrota;
// Rodada 2: vitória", in the documentation language; the rounds are left out
// when its result records none (an exempt or inherited bar), the bar when its
// spec declares none. Unreadable documents read as empty.
func childBarOutcome(repoDir, id string) string {
	var b strings.Builder
	spec, _ := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if bar, err := doccheck.SpecBar(spec); err == nil {
		b.WriteString(" — " + bar)
	}
	result, _ := os.ReadFile(FeatureDocPath(repoDir, id, "result.md"))
	l := store.DocsLanguage(repoDir)
	words := map[string]doclang.Term{"win": doclang.Win, "loss": doclang.Loss, "tie": doclang.Tie}
	var rounds []string
	for _, r := range lint.GauntletRoundList(result) {
		if r.N > 0 && r.Outcome != "" {
			rounds = append(rounds, fmt.Sprintf("%s %d: %s", doclang.Display(doclang.Round, l), r.N, doclang.Display(words[r.Outcome], l)))
		}
	}
	if len(rounds) > 0 {
		b.WriteString(" — " + strings.Join(rounds, "; "))
	}
	return b.String()
}

// childrenOf is parentID's direct children that keep accepts, sorted by id.
func childrenOf(repoDir, parentID string, keep func(store.Feature) bool) ([]store.Feature, error) {
	all, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	var out []store.Feature
	for _, c := range store.ChildrenByParent(all)[parentID] {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// keptByHumanGate is the cascade of an actor the human gate refuses: no child
// carried and, when some child would have been, the line saying why — the UI
// started from an agent session reaches the core with that actor.
func keptByHumanGate(parentID string, waiting []store.Feature) cascaded {
	if len(waiting) == 0 {
		return cascaded{}
	}
	ids := make([]string, len(waiting))
	for i, c := range waiting {
		ids[i] = c.ID
	}
	return cascaded{lines: []string{fmt.Sprintf("children not carried along with %s: the human gate refuses this actor (CERNE_ACTOR=agent) — %s stay as they are, for a human to move", parentID, strings.Join(ids, ", "))}}
}

// moveChild applies action to child c through the FSM, persists it and
// appends its lifecycle event.
func moveChild(repoDir string, c *store.Feature, action domain.Action, payload map[string]string, actor string) error {
	to, err := domain.Transition(c.State, action)
	if err != nil {
		return err
	}
	c.State = to
	c.UpdatedAt = time.Now().UTC()
	if err := store.SaveFeature(repoDir, *c); err != nil {
		return err
	}
	return store.AppendEvent(repoDir, domain.NewEvent(c.ID, eventTypes[action], payload, actor))
}
