package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
)

// newFeatureMirrorCmd is the human opt-in that publishes a card to a tracker (F-0449).
func newFeatureMirrorCmd(dir *string) *cobra.Command {
	var title, summary, trackerName string
	cmd := &cobra.Command{
		Use:   "mirror <id> --title <public title> --summary <public summary>",
		Short: "Publish a card to an issue tracker with a public title and summary (human only)",
		Long: "Creates an issue for the card on a tracker of mirror.trackers (default: the first) and links it to the card. " +
			"Only the public title and summary given here, the card id and its state leave the backend; the issue then follows " +
			"the card's state. Needs mirror.enabled; an agent is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			f, err := app.MirrorFeature(ctx.repoDir, ctx.projDir, args[0], trackerName, title, summary)
			if f.Mirror != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "mirrored %s to %s: %s\n", f.ID, f.Mirror.Tracker, f.Mirror.URL)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "the issue's public title")
	cmd.Flags().StringVar(&summary, "summary", "", "the issue's public summary")
	cmd.Flags().StringVar(&trackerName, "tracker", "", "the mirror.trackers name to publish to (default: the first)")
	return cmd
}
