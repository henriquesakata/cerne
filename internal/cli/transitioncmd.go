package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
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
	{"approve", "Approve a candidate feature (requires a real spec)", domain.ActionApprove,
		"  # gate: the spec must be filled in (not the stub) and pushed\n" +
			"  cerne push && cerne feature approve <id>\n" + lifecycleSeq},
	{"reject", "Reject a candidate feature (requires --reason)", domain.ActionReject, ""},
	{"start", "Start development (requires a real plan)", domain.ActionStart,
		"  # gate: the plan must be filled in (not the stub) and pushed\n" +
			"  cerne push && cerne feature start <id>\n" + lifecycleSeq},
	{"complete", "Complete development (requires a real result)", domain.ActionComplete,
		"  # gate: the result must be filled in (not the stub) and pushed\n" +
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
		var by, reason string
		var force bool
		cmd := &cobra.Command{
			Use:     ts.use + " <id>",
			Short:   ts.short,
			Example: ts.example,
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTransition(cmd, *dir, ts.action, args[0], by, reason, force)
			},
		}
		cmd.Flags().StringVar(&by, "by", "", "id of the superseding feature (substituir only)")
		cmd.Flags().StringVar(&reason, "reason", "", "reason for the transition (reject, substituir and revert)")
		cmd.Flags().BoolVar(&force, "force", false, "approve even with open [ESCLARECER] clarifications in the spec (approve only)")
		cmds = append(cmds, cmd)
	}
	return cmds
}

func runTransition(cmd *cobra.Command, dir string, action domain.Action, id, by, reason string, force bool) error {
	// Validate flags before resolveProject so bad invocations fail fast
	// without touching origin.
	if err := app.ValidateTransitionFlags(action, by, reason); err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	// Approve is the start-of-flow human gate (F-0131 crit. 9): checked here,
	// in the CLI, rather than inside app.TransitionFeature — the server/UI path
	// is a human interaction by definition and carries no such check. Revert
	// undoes that human act, so it stands behind the same kind of gate (F-0169).
	if action == domain.ActionApprove {
		if err := app.CheckHumanGate(ctx.cfg.Gates.ApproveGate()); err != nil {
			return err
		}
		// Open [ESCLARECER] markers mean the spec still has questions for the
		// human — the template's "none remaining before approve" rule as a gate,
		// not prose (F-0175 crit. 7). Same CLI-side placement as the human gate:
		// the UI path renders the markers as chips the human answers directly.
		if !force {
			if err := app.CheckOpenClarifications(ctx.repoDir, id); err != nil {
				return err
			}
		}
	}
	if action == domain.ActionRevert {
		if err := app.CheckHumanGate(ctx.cfg.Gates.RevertGate()); err != nil {
			return err
		}
	}
	out, err := app.TransitionFeature(ctx.repoDir, ctx.projDir, id, action, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	// Starting a child pulls its ready ancestor epics into development too:
	// report them so the author sees the board moved without a second command.
	for _, epic := range out.Propagated {
		fmt.Fprintf(cmd.OutOrStdout(), "  ↳ epic %s started along with it (a child entered development)\n", epic)
	}
	// Approving a feature scaffolds its plan stub; starting scaffolds its result
	// stub: point the author at the doc the next gate will require.
	if action == domain.ActionApprove {
		fmt.Fprintf(cmd.OutOrStdout(), "Next step: fill in the plan at content/features/%s/plan.md ('cerne plan draft %s' scaffolds it) and run 'cerne push' before 'cerne feature start'.\n", id, id)
	}
	if action == domain.ActionStart {
		fmt.Fprintf(cmd.OutOrStdout(), "Next step: when done, fill in the result at content/features/%s/result.md ('cerne result draft %s' scaffolds it) and run 'cerne push' before 'cerne feature complete'.\n", id, id)
	}
	return nil
}
