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
const lifecycleSeq = "  # ciclo de vida: new → approve → start → complete"

var transitionSpecs = []transitionSpec{
	{"approve", "Approve a candidate feature (requires a real spec)", domain.ActionApprove,
		"  # gate: a spec precisa estar preenchida (não o stub) e pushada\n" +
			"  cerne push && cerne feature approve <id>\n" + lifecycleSeq},
	{"reject", "Reject a candidate feature (requires --reason)", domain.ActionReject, ""},
	{"start", "Start development (requires a real plan)", domain.ActionStart,
		"  # gate: o plano precisa estar preenchido (não o stub) e pushado\n" +
			"  cerne push && cerne feature start <id>\n" + lifecycleSeq},
	{"complete", "Complete development (requires a real result)", domain.ActionComplete,
		"  # gate: o resultado precisa estar preenchido (não o stub) e pushado\n" +
			"  cerne push && cerne feature complete <id>\n" + lifecycleSeq},
	{"substituir", "Substituir uma feature por outra (requer --by e --reason)", domain.ActionSupersede, ""},
}

func newTransitionCmds(dir *string) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(transitionSpecs))
	for _, ts := range transitionSpecs {
		var by, reason string
		cmd := &cobra.Command{
			Use:     ts.use + " <id>",
			Short:   ts.short,
			Example: ts.example,
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runTransition(cmd, *dir, ts.action, args[0], by, reason)
			},
		}
		cmd.Flags().StringVar(&by, "by", "", "id of the superseding feature (substituir only)")
		cmd.Flags().StringVar(&reason, "reason", "", "reason for the transition (reject and substituir)")
		cmds = append(cmds, cmd)
	}
	return cmds
}

func runTransition(cmd *cobra.Command, dir string, action domain.Action, id, by, reason string) error {
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
	// is a human interaction by definition and carries no such check.
	if action == domain.ActionApprove {
		if err := app.CheckHumanGate(ctx.cfg.Gates.ApproveGate()); err != nil {
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
		fmt.Fprintf(cmd.OutOrStdout(), "  ↳ épico %s iniciado junto (filho entrou em desenvolvimento)\n", epic)
	}
	// Approving a feature scaffolds its plan stub; starting scaffolds its result
	// stub: point the author at the doc the next gate will require.
	if action == domain.ActionApprove {
		fmt.Fprintf(cmd.OutOrStdout(), "Próximo passo: preencha o plano em content/features/%s/plan.md (cerne plan draft %s ajuda) e rode 'cerne push' antes de 'cerne feature start'.\n", id, id)
	}
	if action == domain.ActionStart {
		fmt.Fprintf(cmd.OutOrStdout(), "Próximo passo: ao concluir, preencha o resultado em content/features/%s/result.md (cerne result draft %s ajuda) e rode 'cerne push' antes de 'cerne feature complete'.\n", id, id)
	}
	return nil
}
