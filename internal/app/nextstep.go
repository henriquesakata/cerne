package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// Actors of a next step (F-0371): who has to move.
const (
	ActorAgent = "agent"
	ActorHuman = "human"
)

// Step is the one next step of a feature: where it stands, who acts, what to
// do, and the exact command when the step is one. Command is empty for a human
// wait, for a terminal state, and for a step with no single command.
type Step struct {
	State   string `json:"state"`
	Actor   string `json:"actor"`
	Step    string `json:"step"`
	Command string `json:"command"`
}

// ImplementedOwes lists the exact commands an implemented feature still owes
// the agent (F-0371): the cost stamp (only with a session bound — NeedsStamp)
// and the delivery link (neither commit nor MR recorded). Empty means the
// feature waits on the human accept. The project queue and NextStep share it.
func ImplementedOwes(repoDir string, f store.Feature) []string {
	var cmds []string
	if cost.NeedsStamp(filepath.Dir(repoDir), repoDir, f.ID) {
		cmds = append(cmds, "cerne cost "+f.ID+" --stamp")
	}
	if f.DeliveryCommitURL == "" && f.DeliveryMRURL == "" {
		cmds = append(cmds, "cerne feature deliver "+f.ID+" --mr <url>")
	}
	return cmds
}

// NextStep decides the next step of feature id by composing the gates the
// transition commands already run — it adds no rule of its own, so it never
// sends the agent to a command the gate refuses. Read-only.
func NextStep(repoDir, projDir, id string) (Step, error) {
	id = domain.CanonicalID(id)
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		return Step{}, fmt.Errorf("%s is not a feature in this project", id)
	}
	if err != nil {
		return Step{}, err
	}
	s := Step{State: string(f.State), Actor: ActorAgent}
	human := func(msg string) (Step, error) {
		s.Actor, s.Step = ActorHuman, msg
		return s, nil
	}
	switch f.State {
	case domain.StateCandidate:
		imps, err := ApproveReadiness(repoDir, projDir, id)
		if err != nil {
			return Step{}, err
		}
		for _, im := range imps {
			if im.Hard {
				s.Step = im.Message + " — fix content/features/" + id + "/spec.md and run 'cerne push'"
				s.Command = "cerne push"
				return s, nil
			}
		}
		var open []string
		for _, im := range imps {
			if im.Code == ImpedimentOpenClarification {
				open = append(open, im.Message)
				continue
			}
			// A card impediment is the agent's: the CLI approve refuses it.
			s.Step = im.Message
			return s, nil
		}
		msg := "the spec is ready: a human runs 'cerne feature approve " + id + "'"
		if len(open) > 0 {
			msg += "; open clarifications: " + strings.Join(open, " | ")
		}
		return human(msg)
	case domain.StateApproved:
		if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
			return human("the spec changed since approval: waiting for a human to re-approve with 'cerne feature approve " + id + "'")
		}
		docs, err := FeatureDocSet(repoDir, id)
		if err != nil {
			return Step{}, err
		}
		if !docs.HasPlan && !strings.HasPrefix(id, "T-") {
			s.Step = "write the plan at content/features/" + id + "/plan.md and run 'cerne push'"
			s.Command = "cerne push"
			return s, nil
		}
		s.Step, s.Command = "start the implementation", "cerne feature start "+id
	case domain.StateInDevelopment:
		err := CheckResultRecord(repoDir, f, projectGates(projDir))
		if err == nil {
			err = CheckPaidCited(repoDir, f)
		}
		if err != nil {
			s.Step = "the complete would refuse: " + err.Error() + " — fix content/features/" + id + "/result.md and run 'cerne push'"
			s.Command = "cerne push"
			return s, nil
		}
		s.Step, s.Command = "complete the feature", "cerne feature complete "+id
	case domain.StateImplemented:
		if cmds := ImplementedOwes(repoDir, f); len(cmds) > 0 {
			s.Step, s.Command = "finish the delivery: "+strings.Join(cmds, "; "), cmds[0]
			return s, nil
		}
		return human("cost and delivery are in: waiting for a human to run 'cerne feature accept " + id + "'")
	default:
		s.Actor, s.Step = "", "no next step: the feature is "+string(f.State)
	}
	return s, nil
}
