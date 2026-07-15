package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

// newFeatureDeliverCmd is the post-hoc delivery capture (F-0179): the
// definitive commit SHA often only exists after the MR's squash-merge, past
// the complete — this records it without touching state or documents.
func newFeatureDeliverCmd(dir *string) *cobra.Command {
	var commit, mr string
	cmd := &cobra.Command{
		Use:   "deliver <id>",
		Short: "Record the delivery commit/MR links on a feature (post-hoc)",
		Example: "  # after the MR merges, link the definitive commit to the feature\n" +
			"  cerne feature deliver F-0042 --commit 1a2b3c4 --mr https://gitlab.com/org/repo/-/merge_requests/7\n" +
			"  # bare SHA needs [delivery] commit_url = \"https://…/{sha}\" in .cernerc; a full URL always works",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if commit == "" && mr == "" {
				return fmt.Errorf("nothing to record: pass --commit and/or --mr")
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			d, err := app.ResolveDelivery(ctx.cfg, commit, mr)
			if err != nil {
				return err
			}
			if err := app.SetFeatureDelivery(ctx.repoDir, ctx.projDir, args[0], d); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s delivery recorded", args[0])
			if d.CommitURL != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " — commit %s", d.CommitURL)
			}
			if d.MRURL != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " — mr %s", d.MRURL)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "delivery commit: full URL, or bare SHA resolved via [delivery] commit_url")
	cmd.Flags().StringVar(&mr, "mr", "", "delivery MR/PR URL")
	return cmd
}
