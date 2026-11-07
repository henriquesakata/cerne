package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
)

func newFeatureReparentCmd(dir *string) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "reparent <id> <parent-id>",
		Short: "Move a feature under an epic (parent), or --clear to detach it",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReparent(cmd, *dir, args, clear)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "detach the feature from its epic")
	return cmd
}

func runReparent(cmd *cobra.Command, dir string, args []string, clear bool) error {
	id := args[0]
	parent := ""
	switch {
	case clear:
		if len(args) > 1 {
			return fmt.Errorf("--clear does not take a <parent-id>")
		}
	case len(args) == 2:
		parent = args[1]
	default:
		return fmt.Errorf("pass the <parent-id> or use --clear")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if err := app.SetFeatureParent(ctx.repoDir, ctx.projDir, id, parent); err != nil {
		return err
	}
	if parent == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s detached from its epic.\n", id)
	} else {
		parent = domain.CanonicalID(parent) // as stored
		fmt.Fprintf(cmd.OutOrStdout(), "%s is now a child of %s.\n", id, parent)
		if app.EpicPlanStub(ctx.repoDir, parent) {
			fmt.Fprintf(cmd.OutOrStdout(), "plano do épico scaffoldado — preencha antes do approve (N-0031)\n")
		}
	}
	return nil
}
