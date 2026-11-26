package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/materialize"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Actors of a next step (F-0371): who has to move. ActorNone is a terminal
// feature: nobody does.
const (
	ActorAgent = "agent"
	ActorHuman = "human"
	ActorNone  = "none"
)

// CostRoots are the Claude Code config dirs the cost precheck reads
// transcripts from; a test points it at a fixture.
var CostRoots = cost.ClaudeRoots

// Step is the one next step of a feature: where it stands, who acts, what to
// do, and the exact command when the step is one. Command is empty when the
// step is an edit, a human wait, or there is none.
type Step struct {
	State   string `json:"state"`
	Actor   string `json:"actor"`
	Step    string `json:"step"`
	Command string `json:"command"`
	// Commands is every command an implemented feature still owes (Command is
	// the first); not part of the JSON contract.
	Commands []string `json:"-"`
}

// ImplementedOwes lists what an implemented feature still owes the agent
// (F-0371): the exact commands — the cost stamp when `cerne cost --stamp` would
// run (cost.PrecheckStamp, the command's own precheck), the delivery link when
// neither commit nor MR is recorded. "Nothing to stamp" and a session bound
// under a harness without a cost reader (B-0053) owe nothing: no command could
// pay them. A non-nil problem is a cost check that failed for any other reason
// (an unreadable ledger): it must show, never fall through to the human
// accept. Empty and nil means the feature waits on the accept. The project
// queue and NextStep share it.
func ImplementedOwes(repoDir string, f store.Feature) (cmds []string, problem error) {
	if !cost.Stamped(repoDir, f.ID) {
		_, err := cost.PrecheckStamp(cost.Inputs{CacheDir: filepath.Dir(repoDir), RepoDir: repoDir, Roots: CostRoots()}, f.State, f.ID)
		switch {
		case err == nil:
			cmds = append(cmds, "cerne cost "+f.ID+" --stamp")
		case errors.Is(err, cost.ErrNothingToStamp), errors.Is(err, cost.ErrUnsupportedHarness):
			// nothing to stamp, or no cost reader for that harness: no debt the agent can pay (B-0053)
		default:
			problem = fmt.Errorf("cost check failed: %w", err)
		}
	}
	if cmd := DeliveryOwed(f); cmd != "" {
		cmds = append(cmds, cmd)
	}
	return cmds, problem
}

// DeliveryOwed is the command that records a missing delivery link, or "" when
// a commit or MR is already on the card. The complete's Next step and
// ImplementedOwes share it (B-0070), so the two never ask different things.
func DeliveryOwed(f store.Feature) string {
	if f.DeliveryCommitURL != "" || f.DeliveryMRURL != "" {
		return ""
	}
	return "cerne feature deliver " + f.ID + " --mr <url>"
}

// docRef names the file the agent edits: the materialized copy under .docs/
// as it is on disk (a title renamed since the pull keeps the old folder), else
// where the next pull puts it — never the backend path (B-0074).
func docRef(projDir string, f store.Feature, name string) string {
	dir := filepath.Join(projDir, ".docs", "current", "features")
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			if e.IsDir() && (e.Name() == f.ID || strings.HasPrefix(e.Name(), f.ID+"-")) {
				return ".docs/current/features/" + e.Name() + "/" + name
			}
		}
	}
	return materialize.ProjectedDocPath(f.ID, f.Title, name)
}

// EditDoc is how every refusal and Next step tells the user to change feature
// f's document filename: the copy 'cerne pull' materializes, then 'cerne
// push' — never the backend path, which the user does not edit (B-0074).
func EditDoc(f store.Feature, filename string) string {
	return "run 'cerne pull', edit " + materialize.ProjectedDocPath(f.ID, f.Title, filename) + " and run 'cerne push'"
}

// NextStepTail is " — next: <step> (run: <command>)" for id, or "" when the
// step cannot be read: the end of the wait's line and of a card notice
// (F-0504), so the two never drift apart.
func NextStepTail(repoDir, projDir, id string) string {
	st, err := NextStep(repoDir, projDir, id)
	if err != nil {
		return ""
	}
	tail := " — next: " + st.Step
	if st.Command != "" {
		tail += " (run: " + st.Command + ")"
	}
	return tail
}

// NextStep decides the next step of feature id by CALLING the checks the
// transitions run (ApproveReadiness, CheckStartNotStale, checkActionDocs,
// checkTaskBar, guardParallelStart, CheckCompleteResult, PrecheckStamp) — it copies none, so
// it never sends the agent to a command a gate refuses; the start's plan
// warning (F-0377) rides on its step. Read-only.
func NextStep(repoDir, projDir, id string) (Step, error) {
	given := id
	id = domain.CanonicalID(id)
	if !domain.IsFeatureID(id) {
		return Step{}, fmt.Errorf("%s is not a feature id (F-/E-/T-)", given)
	}
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return Step{}, fmt.Errorf("%s is not a feature in this project", given)
	}
	if err != nil {
		return Step{}, err
	}
	gates := projectGates(projDir)
	s := Step{State: string(f.State), Actor: ActorAgent}
	edit := func(msg, doc string) (Step, error) {
		s.Step = msg + " — edit " + docRef(projDir, f, doc) + " and run 'cerne push'"
		return s, nil
	}
	// A human act: with the project's gate set to auto it is the agent's, with the command.
	humanAct := func(gate, action, why string) (Step, error) {
		cmd := "cerne feature " + action + " " + id
		if gate == project.GateAuto {
			s.Step, s.Command = why, cmd
			return s, nil
		}
		s.Actor, s.Step = ActorHuman, why+": a human runs '"+cmd+"'"
		return s, nil
	}
	switch f.State {
	case domain.StateCandidate:
		imps, err := ApproveReadiness(repoDir, projDir, id)
		if err != nil {
			return Step{}, err
		}
		var open []string
		for _, im := range imps {
			switch {
			case im.Code == ImpedimentOpenClarification:
				open = append(open, im.Message)
			case im.Hard:
				return edit(im.Code+": "+im.Message, "spec.md")
			default:
				s.Step = im.Code + ": " + im.Message + " (yours to fix)"
				return s, nil
			}
		}
		why := "the spec is ready"
		if len(open) > 0 {
			why += "; open clarifications: " + strings.Join(open, " | ")
		}
		return humanAct(gates.ApproveGate(), "approve", why)
	case domain.StateApproved:
		if err := CheckStartNotStale(repoDir, f); err != nil {
			return humanAct(gates.ApproveGate(), "approve", "the spec changed since approval: it needs a re-approval")
		}
		docs, err := FeatureDocSet(repoDir, id)
		if err != nil {
			return Step{}, err
		}
		if err := checkActionDocs(f, domain.ActionStart, docs); err != nil {
			return edit("the start would refuse: "+errors.Unwrap(err).Error(), "plan.md")
		}
		if gates.GauntletEnabled() {
			if err := checkTaskBar(repoDir, f); err != nil {
				return edit("the start would refuse: "+err.Error(), "spec.md")
			}
		}
		if _, _, err := guardParallelStart(repoDir, projDir, id, false); err != nil {
			s.Step = "the start would refuse: " + err.Error()
			return s, nil
		}
		s.Step, s.Command = "start the implementation", "cerne feature start "+id
		if w := planBehindWarning(repoDir, f); w != "" {
			s.Step += " — " + w
		}
	case domain.StateInDevelopment:
		docs, err := FeatureDocSet(repoDir, id)
		if err != nil {
			return Step{}, err
		}
		if err := checkActionDocs(f, domain.ActionComplete, docs); err != nil {
			return edit("the complete would refuse: "+errors.Unwrap(err).Error(), "result.md")
		}
		if err := CheckCompleteResult(repoDir, f, gates); err != nil {
			s.Step = "the complete would refuse: " + err.Error() // the error already says what to edit (EditDoc)
			return s, nil
		}
		if err := CheckCriterionTests(repoDir, projDir, f); err != nil {
			s.Step = "the complete would refuse: " + err.Error()
			return s, nil
		}
		s.Step, s.Command = "complete the feature", "cerne feature complete "+id
		if gates.AcceptGate() == project.GateRisk { // read from the gate alone: no diff here (F-0516 crit. 11)
			s.Step += " — under the risk gate it is accepted on its own only if it touches no sensitive path or facet, its Gauntlet ended in a win and every criterion has a test; otherwise it waits for the human merge of its PR"
		}
	case domain.StateImplemented:
		cmds, problem := ImplementedOwes(repoDir, f)
		switch {
		case problem != nil:
			s.Step = problem.Error()
		case len(cmds) > 0:
			s.Step, s.Command, s.Commands = "finish the delivery: "+strings.Join(cmds, "; "), cmds[0], cmds
		// Under the merge gate (F-0488) the human acts on the forge; recording
		// the merge is the agent's. NextStep stays offline: --merged asks. A
		// card the risk gate held (F-0516) waits the same way.
		case gates.AcceptsOnMerge() && f.DeliveryMRURL == "":
			s.Step = "link the delivery PR: under the " + gates.AcceptGate() + " gate its merge is the acceptance; for a delivery with no PR, a human runs 'cerne feature accept " + id + "'"
			s.Command = "cerne feature deliver " + id + " --mr <url>"
		case gates.AcceptsOnMerge():
			s.Step = "the human's merge of " + f.DeliveryMRURL + " into the default branch is the acceptance; after it, record it"
			s.Command = "cerne feature accept " + id + " --merged"
		default:
			return humanAct(gates.AcceptGate(), "accept", "cost and delivery are in")
		}
	case domain.StateDone, domain.StateRejected, domain.StateSuperseded:
		s.Actor, s.Step = ActorNone, "no next step: the feature is "+string(f.State)
	default:
		return Step{}, fmt.Errorf("%w: %q on %s", domain.ErrUnknownState, f.State, id)
	}
	return s, nil
}
