package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// transitionSpec wires one lifecycle subcommand to its domain action.
type transitionSpec struct {
	use     string
	short   string
	action  domain.Action
	example string // shown in --help: the gate precondition + lifecycle sequence
}

// lifecycleSeq is the feature lifecycle, repeated in each gate example so the
// reader always sees where the transition sits.
const lifecycleSeq = "  # lifecycle: new → approve → start → complete"

var transitionSpecs = []transitionSpec{
	{"approve", "Approve a candidate, or re-approve an approved feature whose spec changed (requires a real spec)", domain.ActionApprove,
		"  # gate: the spec must be filled in (not the stub), name its bar, and be pushed\n" +
			"  cerne feature approve <id> --check   # rehearsal: lists every impediment, writes nothing\n" +
			"  cerne push && cerne feature approve <id>\n" +
			"  # an approved spec edited since its approval blocks the start until re-approved:\n" +
			"  cerne feature approve <id>           # renews the approval with the current spec\n" + lifecycleSeq},
	{"reject", "Reject a candidate feature (requires --reason)", domain.ActionReject, ""},
	{"start", "Start development (requires a real plan)", domain.ActionStart,
		"  # gate: the plan must be filled in (not the stub) and pushed\n" +
			"  cerne push && cerne feature start <id>\n" + lifecycleSeq},
	{"complete", "Complete development (requires a real result)", domain.ActionComplete,
		"  # gate: the result must be filled in (not the stub) and pushed, cite\n" +
			"  # \"critério N\" for each numbered spec criterion, and carry a\n" +
			"  # \"## Gauntlet\" record (cited verdict, or the literal exemption)\n" +
			"  cerne push && cerne feature complete <id>\n" + lifecycleSeq},
	{"substituir", "Supersede a feature with another (requires --by and --reason)", domain.ActionSupersede, ""},
	{"revert", "Send an approved feature back to candidate (requires --reason)", domain.ActionRevert,
		"  # undo of an approval: state moves back, the event keeps the why,\n" +
			"  # spec and plan stay intact\n" +
			"  cerne feature revert <id> --reason \"approved by mistake\"\n" + lifecycleSeq},
}

func newTransitionCmds(dir *string) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(transitionSpecs))
	for _, ts := range transitionSpecs {
		var by, reason, commit, mr string
		var force, check, noStamp bool
		cmd := &cobra.Command{
			Use:     ts.use + " <id>",
			Short:   ts.short,
			Example: ts.example,
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTransition(cmd, *dir, ts.action, args[0], by, reason, commit, mr, force, check, noStamp)
			},
		}
		cmd.Flags().StringVar(&by, "by", "", "id of the superseding feature (substituir only)")
		cmd.Flags().StringVar(&reason, "reason", "", "reason for the transition (reject, substituir and revert)")
		cmd.Flags().StringVar(&commit, "commit", "", "delivery commit: full URL, or bare SHA resolved via [delivery] commit_url (complete only)")
		cmd.Flags().StringVar(&mr, "mr", "", "delivery MR/PR: full URL, or #<n>/!<n> via [delivery] pr_url (complete only)")
		cmd.Flags().BoolVar(&force, "force", false, "approve with open [ESCLARECER] clarifications (approve), or start despite another session's active work (start)")
		if ts.action == domain.ActionComplete {
			cmd.Flags().BoolVar(&noStamp, "no-stamp", false, "complete without stamping the cost; run 'cerne cost <id> --stamp' later (F-0402)")
		}
		if ts.action == domain.ActionApprove {
			cmd.Flags().BoolVar(&check, "check", false, "list every impediment to approving without approving — no human gate, nothing written (F-0308)")
		}
		cmds = append(cmds, verbs.MarkLifecycle(cmd)) // every transition is lifecycle (F-0323)
	}
	return cmds
}

func runTransition(cmd *cobra.Command, dir string, action domain.Action, id, by, reason, commit, mr string, force, check, noStamp bool) error {
	// Validate flags before resolveProject so bad invocations fail fast
	// without touching origin.
	if err := app.ValidateTransitionFlags(action, by, reason); err != nil {
		return err
	}
	id = domain.CanonicalID(id) // before anything loads the entity: Linux file names are case-sensitive
	if (commit != "" || mr != "") && action != domain.ActionComplete {
		return fmt.Errorf("--commit/--mr are only allowed for complete, not %s", action)
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	// Delivery links (F-0179): resolved and validated before the transition so
	// a bad link never leaves a half-applied complete.
	var delivery *app.Delivery
	if commit != "" || mr != "" {
		d, err := app.ResolveDelivery(ctx.cfg, commit, mr)
		if err != nil {
			return err
		}
		// With a [delivery] forge, ask it for the PR (F-0355); a failure only
		// prints how to link it by hand.
		if line := app.DiscoverPR(ctx.cfg, ctx.projDir, id, &d); line != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), line)
		}
		delivery = &d
	}
	// The approve rehearsal (F-0308 crit. 3) reads, never writes, so it comes
	// before the human gate: the agent that may not approve is exactly the one
	// that needs to know what still stops the human.
	if check {
		return runApproveCheck(cmd.OutOrStdout(), ctx, id)
	}
	// Approve is the start-of-flow human gate (F-0131 crit. 9): checked here,
	// in the CLI, rather than inside app.TransitionFeature — the server/UI path
	// is a human interaction by definition and carries no such check. Revert
	// undoes that human act, so it stands behind the same kind of gate (F-0169).
	if action == domain.ActionApprove {
		if err := app.CheckHumanGate(ctx.cfg.Gates.ApproveGate()); err != nil {
			return err
		}
		// The Gauntlet bar gate is not here: it lives in the shared transition
		// core so the UI enforces it too (F-0295), and --force never reaches it.
		// Open [ESCLARECER] markers mean the spec still has questions for the
		// human — the template's "none remaining before approve" rule as a gate,
		// not prose (F-0175 crit. 7). Same CLI-side placement as the human gate:
		// the UI path renders the markers as chips the human answers directly.
		if !force {
			if err := app.CheckOpenClarifications(ctx.repoDir, ctx.projDir, id); err != nil {
				return err
			}
			// A card with no tag, or no capability while the project curates a
			// taxonomy, is the agent's half-registration (F-0336, N-0028): same
			// CLI-side placement, same --force escape, the core never refuses it.
			if err := app.CheckIncompleteCard(ctx.repoDir, ctx.projDir, id); err != nil {
				return err
			}
		}
	}
	if action == domain.ActionRevert {
		if err := app.CheckHumanGate(ctx.cfg.Gates.RevertGate()); err != nil {
			return err
		}
	}
	out, err := app.TransitionFeatureForce(ctx.repoDir, ctx.projDir, id, action, by, reason, delivery, force)
	if err != nil {
		return err
	}
	// Parallel-implementation guard advisories (F-0257 crit. 4): same actor, no
	// session evidence — the start proceeds, the human gets told.
	for _, w := range out.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), w)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	printMovedPendings(cmd.OutOrStdout(), out.Pendings)
	// Say what the complete recorded and where it shows (F-0355).
	if delivery != nil && *delivery != (app.Delivery{}) {
		if f, err := store.LoadFeature(ctx.repoDir, id); err == nil {
			if line := app.DeliveryLine(f, true); line != "" {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
		}
	}
	// Approving is accepting the proposed bar (F-0308 crit. 9): say which.
	if out.Bar != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Accepted bar: %s\n", out.Bar)
	}
	if action == domain.ActionApprove {
		if err := printJudgeQuestionAccepted(cmd.OutOrStdout(), ctx, id, out); err != nil {
			return err
		}
	}
	// Starting a child pulls its ready ancestor epics into development too:
	// report them so the author sees the board moved without a second command.
	for _, epic := range out.Propagated {
		fmt.Fprintf(cmd.OutOrStdout(), "  ↳ epic %s started along with it (a child entered development)\n", epic)
	}
	// Approving a feature scaffolds its plan stub; starting scaffolds its result
	// stub: point the author at the doc the next gate will require. A
	// re-approval or a baseline record (F-0318) says so, and says the start is
	// open only when its document gate would pass.
	if action == domain.ActionApprove {
		switch {
		case out.Baseline:
			fmt.Fprintln(cmd.OutOrStdout(), "Approval baseline recorded: the current spec is now the approved version.")
		case out.From == domain.StateApproved:
			fmt.Fprintln(cmd.OutOrStdout(), "Approval renewed: the current spec is now the approved version.")
		}
		docs, err := app.FeatureDocSet(ctx.repoDir, id)
		if out.From == domain.StateApproved && err == nil && domain.CheckGate(id, domain.ActionStart, docs) == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Next step: 'cerne feature start %s' is open again.\n", id)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Next step: fill in the plan at content/features/%s/plan.md ('cerne plan draft %s' scaffolds it) and run 'cerne push' before 'cerne feature start'.\n", id, id)
		}
	}
	if action == domain.ActionComplete {
		// The transition is the act that counts: a stamp that cannot be made
		// warns and the exit stays 0 (F-0402 crit. 3).
		if !noStamp {
			o, serr := stampCost(ctx, id)
			fmt.Fprintln(cmd.OutOrStdout(), completeStampLine(id, o, serr))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Next step: record its token cost with 'cerne cost %s --stamp' before the human accept (F-0309).\n", id)
		}
		printCompleteLearnings(cmd.OutOrStdout(), ctx.repoDir, id)
	}
	if action == domain.ActionStart {
		fmt.Fprintf(cmd.OutOrStdout(), "Next step: when done, fill in the result at content/features/%s/result.md ('cerne result draft %s' scaffolds it) and run 'cerne push' before 'cerne feature complete'.\n", id, id)
	}
	// Completing the last alive child of an epic scaffolds the epic's result
	// (F-0214): point the author at it so the closure isn't left to be noticed
	// later — the complete gate still requires a real result before it fires.
	if out.EpicReadyToClose != "" {
		epic := out.EpicReadyToClose
		fmt.Fprintf(cmd.OutOrStdout(), "↳ epic %s ready to close: fill content/features/%s/result.md and run 'cerne feature complete %s'\n", epic, epic, epic)
	}
	return nil
}

// errCheckImpediments is the non-zero exit of an approve --check whose list
// is not empty — hard impediments, or only open clarifications for a human.
var errCheckImpediments = errors.New("approve check found impediments")

// barForms are the three literal bar forms (cerne-authoring skill, F-0270),
// taught by the check instead of every refusal (F-0308 crit. 11). They stay
// pt-BR: they are text written inside the spec.
const barForms = `The spec's Verificação section needs one of the three literal forms:
  Barra: <referência nomeada + acesso (URL, comando ou arquivo que um crítico abre)>
  Barra: não aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste/benchmark/invariante>
  Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)
`

// lightModeApprove replaces the accepted-bar line when the project runs
// without the Gauntlet (F-0364): a ready spec then has no bar to accept.
const lightModeApprove = `light mode: no Gauntlet bar or judge question is charged ([gates] gauntlet = "off" in .cernerc)`

// barExemptApprove says why a bar that is "não aplicável" or waived owes no
// judge question (F-0381).
const barExemptApprove = "bar not applicable/waived: no judge question is charged"

// judgeQuestionForms are the accepted forms of the question line, taught by
// the check like the bar's three (F-0381). They stay as written in the spec.
const judgeQuestionForms = `Under the bar, in the spec's Verificação section, one line:
  Pergunta ao juiz: <a pergunta que o juiz cego responde>
  Question for the judge: <the question the blind judge answers>
`

// printJudgeQuestionAccepted ends an approve's confirmation with what the
// approve accepted beside the bar, or why no question was charged (F-0381).
func printJudgeQuestionAccepted(w io.Writer, ctx projCtx, id string, out app.TransitionOutcome) error {
	switch {
	case !ctx.cfg.Gates.GauntletEnabled():
		fmt.Fprintln(w, lightModeApprove)
	case out.JudgeQuestion != "":
		fmt.Fprintf(w, "Accepted judge question: %s\n", out.JudgeQuestion)
	case out.Bar != "":
		if _, exempt, err := app.AcceptedJudgeQuestion(ctx.repoDir, id); err != nil {
			return err
		} else if exempt {
			fmt.Fprintln(w, barExemptApprove)
		}
	}
	return nil
}

// runApproveCheck prints every impediment to approving id and returns
// errCheckImpediments while the list is not empty (crit. 3). Hard impediments
// and the card's (F-0336) are the agent's to fix; open clarifications are questions for the human
// (F-0175), never for the agent to answer — so with only those left the check
// says there is no hard impediment and shows the bar the approve would accept.
// It only reads the backend clone.
func runApproveCheck(w io.Writer, ctx projCtx, id string) error {
	imps, err := app.ApproveReadiness(ctx.repoDir, ctx.projDir, id)
	if err != nil {
		return err
	}
	// Open clarifications are the human's; everything else is the agent's to
	// fix — the hard ones and the card's (F-0336), soft as they are.
	var agent, soft []app.ApproveImpediment
	hard := 0
	for _, im := range imps {
		if im.Hard {
			hard++
		}
		if im.Code == app.ImpedimentOpenClarification {
			soft = append(soft, im)
		} else {
			agent = append(agent, im)
		}
	}
	if len(agent) > 0 {
		fmt.Fprintf(w, "Check: %s is not approvable yet — %d impediment(s) (nothing approved, nothing written):\n", id, len(imps))
		barProblem, questionProblem := false, false
		for _, im := range imps {
			suffix := ""
			switch {
			case im.Code == app.ImpedimentOpenClarification:
				suffix = " (for a human to answer; the CLI approve refuses it without --force, the UI does not)"
			case !im.Hard:
				suffix = " (yours to fix; the CLI approve refuses it without --force, the UI does not)"
			}
			fmt.Fprintf(w, "  - %s: %s%s\n", im.Code, im.Message, suffix)
			barProblem = barProblem || im.Code == app.ImpedimentBarMissing || im.Code == app.ImpedimentBarMalformed
			questionProblem = questionProblem || im.Code == app.ImpedimentJudgeQuestionMissing || im.Code == app.ImpedimentJudgeQuestionMisplaced
		}
		if barProblem {
			fmt.Fprint(w, "\n"+barForms)
		}
		if questionProblem {
			fmt.Fprint(w, "\n"+judgeQuestionForms)
		}
		if hard > 0 {
			fmt.Fprintf(w, "The check reads the backend: edit the spec (.docs/current/features/%s-*/spec.md), 'cerne push', and check again.\n", id)
		}
		if len(agent) > hard {
			fmt.Fprintln(w, "Complete the card with the commands above, then check again.")
		}
	} else {
		head := "is approvable"
		if len(soft) > 0 {
			head = "has no hard impediment"
		}
		fmt.Fprintf(w, "Check: %s %s (nothing approved, nothing written).\n", id, head)
		// F-0318 crit. 14: approved before the copy existed — the approve records it.
		if f, err := store.LoadFeature(ctx.repoDir, id); err == nil && app.ApprovalBaselineMissing(ctx.repoDir, f) {
			fmt.Fprintf(w, "No approved version is recorded for %s yet: approving records the current spec as its approved version (the baseline later edits are compared with).\n", id)
		}
		if len(soft) > 0 {
			fmt.Fprintf(w, "%d open clarification(s) for a human to answer — the CLI approve refuses them without --force; the UI does not:\n", len(soft))
			for _, im := range soft {
				fmt.Fprintf(w, "  - %s: %s\n", im.Code, im.Message)
			}
		}
		bar, err := app.AcceptedBar(ctx.repoDir, id)
		if err != nil {
			return err
		}
		if bar == "" || !ctx.cfg.Gates.GauntletEnabled() { // light mode (F-0364): no bar is charged
			fmt.Fprintln(w, lightModeApprove)
		} else {
			fmt.Fprintf(w, "Approving accepts its bar:\n  %s\n", bar)
			question, exempt, err := app.AcceptedJudgeQuestion(ctx.repoDir, id)
			if err != nil {
				return err
			}
			switch {
			case exempt:
				fmt.Fprintln(w, barExemptApprove)
			case question != "":
				fmt.Fprintf(w, "Approving accepts its judge question:\n  %s\n", question)
			}
		}
	}
	// Warnings never count as impediments (F-0353): the exit code ignores them.
	warnings, err := app.ApproveWarnings(ctx.repoDir, ctx.projDir, id)
	if err != nil {
		return err
	}
	if len(warnings) > 0 {
		fmt.Fprintln(w, "Warnings (they do not block the approve):")
		for _, msg := range warnings {
			fmt.Fprintf(w, "  - %s\n", msg)
		}
	}
	// Advice never refuses either (F-0390): the exit code ignores it.
	if advice := app.ApproveAdvices(ctx.repoDir, ctx.projDir, id); len(advice) > 0 {
		fmt.Fprintln(w, "Advice (it never blocks the approve):")
		for _, a := range advice {
			fmt.Fprintf(w, "  - %s: %s\n", a.Code, a.Message)
		}
	}
	if len(soft) > 0 {
		fmt.Fprintf(w, "Do not answer them yourself: leave the questions for the human, with the approve request: 'cerne clarify list %s'.\n", id)
	}
	if len(imps) > 0 {
		return fmt.Errorf("%w: %s has %d (%d hard)", errCheckImpediments, id, len(imps), hard)
	}
	if app.CheckHumanGate(ctx.cfg.Gates.ApproveGate()) != nil {
		fmt.Fprintf(w, "Next: ask a human to run 'cerne feature approve %s'.\n", id)
	} else {
		fmt.Fprintf(w, "Next: cerne feature approve %s\n", id)
	}
	return nil
}

// printApproveReadiness ends a spec write with the candidate's approve
// readiness in one line (F-0308 crit. 5): approvable with the bar it would
// accept, or the impediment codes and the check that details them. Past
// candidacy (a task is born approved) there is nothing to approve: it prints
// nothing — unless the write staled the approval (F-0318), whose re-approve
// has the same readiness. An approval with no baseline yet is not staled by a
// write, so it stays silent too.
func printApproveReadiness(w io.Writer, repoDir, projDir, id string) {
	if f, err := store.LoadFeature(repoDir, id); err == nil && app.ApprovalBaselineMissing(repoDir, f) {
		return
	}
	imps, err := app.ApproveReadiness(repoDir, projDir, id)
	if errors.Is(err, domain.ErrInvalidTransition) {
		return
	}
	if err != nil {
		fmt.Fprintf(w, "Approve readiness: unknown (%v)\n", err)
		return
	}
	if len(imps) == 0 {
		bar, err := app.AcceptedBar(repoDir, id)
		if err != nil {
			fmt.Fprintf(w, "Approve readiness: unknown (%v)\n", err)
			return
		}
		if bar == "" {
			fmt.Fprintln(w, "Approve readiness: approvable — "+lightModeApprove)
			return
		}
		fmt.Fprintf(w, "Approve readiness: approvable — approving accepts %s\n", bar)
		return
	}
	var codes []string
	agent := false // anything but an open clarification is the agent's to fix (F-0336)
	for _, im := range imps {
		agent = agent || im.Code != app.ImpedimentOpenClarification
		if !slices.Contains(codes, im.Code) {
			codes = append(codes, im.Code)
		}
	}
	verdict := "not approvable — " + strings.Join(codes, ", ")
	if !agent {
		// Only open clarifications: questions for a human, not the agent (F-0175).
		verdict = "no hard impediment — open-clarification pending for a human"
	}
	fmt.Fprintf(w, "Approve readiness: %s; details: 'cerne feature approve %s --check'\n", verdict, id)
}
