package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
)

// transitionSpec wires one lifecycle subcommand to its domain action.
type transitionSpec struct {
	use    string
	short  string
	action domain.Action
}

var transitionSpecs = []transitionSpec{
	{"approve", "Approve a candidate feature (requires a real spec)", domain.ActionApprove},
	{"reject", "Reject a candidate feature (requires --reason)", domain.ActionReject},
	{"start", "Start development (requires a real plan)", domain.ActionStart},
	{"complete", "Complete development (requires a real result)", domain.ActionComplete},
	{"substituir", "Substituir uma feature por outra (requer --by e --reason)", domain.ActionSupersede},
}

func newTransitionCmds(dir *string) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(transitionSpecs))
	for _, ts := range transitionSpecs {
		var by, reason string
		cmd := &cobra.Command{
			Use:   ts.use + " <id>",
			Short: ts.short,
			Args:  cobra.ExactArgs(1),
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
	out, err := app.TransitionFeature(ctx.repoDir, ctx.projDir, id, action, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}
