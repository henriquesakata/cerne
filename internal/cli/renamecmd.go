package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

func newFeatureRenameCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <id> <novo título>",
		Short: "Rename a feature's title (allowed until development; frozen in terminal states)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			id := args[0]
			if err := app.RenameFeature(ctx.repoDir, ctx.projDir, id, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s renomeada.\n", id)
			return nil
		},
	}
}
