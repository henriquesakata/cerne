package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
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
	id := domain.CanonicalID(args[0]) // echoed as stored
	parent := ""
	switch {
	case clear:
		if len(args) > 1 {
			return fmt.Errorf("--clear does not take a <parent-id>")
		}
	case len(args) == 2:
		if strings.TrimSpace(args[1]) == "" { // detaching is --clear, never a blank id
			return fmt.Errorf("<parent-id> needs a feature id (use --clear to detach)")
		}
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
			fmt.Fprintf(cmd.OutOrStdout(), "epic plan scaffolded — fill it in before approve (N-0031)\n")
		}
	}
	return nil
}
