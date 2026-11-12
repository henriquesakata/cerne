package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
)

// newHandoffCmd is the baton-passing area (F-0368): a handoff recorded by
// `cerne work handoff` carries its own HO- code; the receiver takes it here.
func newHandoffCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Take, list or cancel the handoffs (HO-) works pass on",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newHandoffTakeCmd(&dir), newHandoffCancelCmd(&dir))
	return cmd
}

func newHandoffTakeCmd(dir *string) *cobra.Command {
	var title, note string
	cmd := &cobra.Command{
		Use:   "take <HO-id> [--title <description>] [--note <text>]",
		Short: "Take a pending handoff: open your own work, close the origin, read the brief",
		Long: "Takes a pending handoff in one commit: opens a new active work with the origin's title (or --title) and " +
			"refs, bound to this session ($CERNE_SESSION), marks the handoff taken (by, when, which work, --note), and " +
			"closes the origin as done ('handed off to <new> via <HO>') — an origin no longer active is left as it is. " +
			"Then prints the brief and the next step. A handoff already taken, superseded or cancelled is refused, " +
			"saying why, and nothing is written.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			r, err := app.TakeHandoff(ctx.repoDir, ctx.projDir, filepath.Dir(ctx.repoDir), os.Getenv("CERNE_SESSION"), args[0], title, note)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s taken — %s created (active) — %s (actor: %s)\n", r.Taken.ID, r.ID, r.Title, r.Actor)
			if r.OriginClosed {
				fmt.Fprintf(out, "closed %s (handed off to %s via %s)\n", r.From, r.ID, r.Taken.ID)
			} else {
				fmt.Fprintf(out, "%s was already done: left as it is\n", r.Taken.FromWork)
			}
			if r.Warning != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", r.Warning)
			}
			fmt.Fprintf(out, "brief of %s:\n%s\nnext step: %s\n", r.Taken.ID, r.Brief, r.Taken.Next)
			// The origin's entry is the brief just printed; the crossing refs' follow.
			for _, h := range r.Handoffs {
				if h.HO != r.Taken.ID {
					printHandoff(out, h)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "description of the new work (defaults to the origin's)")
	cmd.Flags().StringVar(&note, "note", "", "what the receiver confirms or adds, recorded on the handoff")
	return cmd
}

func newHandoffCancelCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "cancel <HO-id> --reason <why>",
		Short: "Withdraw a pending handoff, with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			if err := app.CancelHandoff(ctx.repoDir, ctx.projDir, args[0], reason); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s cancelled: %s\n", strings.ToUpper(strings.TrimSpace(args[0])), reason)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the handoff is withdrawn (required)")
	return cmd
}

// printHandoff prints a work's last handoff (F-0339) and how to take it over:
// by its HO- code when it has one (F-0368), else with --from.
func printHandoff(out io.Writer, h app.WorkHandoff) {
	fmt.Fprintf(out, "last handoff of %s (%s, %s):\n%s\n", h.ID, h.Entry.Date, h.Entry.Author, h.Entry.Text)
	switch {
	case !h.Hint:
	case h.HO != "":
		fmt.Fprintf(out, "take it over: cerne handoff take %s\n", h.HO)
	default:
		fmt.Fprintf(out, "take it over: cerne work start \"<description>\" --from %s\n", h.ID)
	}
}
