package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
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

// newMirrorCmd groups the tracker mirror's verbs (F-0449).
func newMirrorCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "mirror",
		Short: "Mirror cards to external issue trackers",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkLocal(newMirrorSyncCmd(&dir)))
	return cmd
}

func newMirrorSyncCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Import outside issues as ideas and bring every mirrored card's issue to its state",
		Long: "Creates an active idea for each open issue of mirror.trackers (filtered by the tracker's label) that no card or " +
			"idea is linked to, then reapplies each mirrored card's state to its issue: the way back from a transition whose " +
			"tracker round failed. Running it again imports nothing new. Exits 1 when a tracker or a card warned.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			rep, err := app.SyncMirror(ctx.repoDir, ctx.projDir)
			out := cmd.OutOrStdout()
			for _, id := range rep.Ideas {
				if i, lerr := store.LoadIdea(ctx.repoDir, id); lerr == nil && i.Mirror != nil {
					fmt.Fprintf(out, "imported %s from %s\n", id, i.Mirror.URL)
				}
			}
			printWarnings(cmd.ErrOrStderr(), rep.Warnings)
			printWarnings(cmd.ErrOrStderr(), rep.Notes) // a note never fails the command
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "synced %d card(s), imported %d idea(s)\n", len(rep.Applied), len(rep.Ideas))
			if len(rep.Warnings) > 0 {
				return fmt.Errorf("mirror sync: %d warning(s)", len(rep.Warnings))
			}
			return nil
		},
	}
}
