package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
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
			return fmt.Errorf("--clear não aceita um <parent-id>")
		}
	case len(args) == 2:
		parent = args[1]
	default:
		return fmt.Errorf("informe o <parent-id> ou use --clear")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if err := app.SetFeatureParent(ctx.repoDir, ctx.projDir, id, parent); err != nil {
		return err
	}
	if parent == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s desvinculada do épico.\n", id)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s agora é filha de %s.\n", id, parent)
	}
	return nil
}
