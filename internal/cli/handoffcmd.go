package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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
	cmd.AddCommand(newHandoffTakeCmd(&dir), newHandoffListCmd(&dir), newHandoffCancelCmd(&dir))
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
			switch {
			case r.OriginClosed:
				fmt.Fprintf(out, "closed %s (handed off to %s via %s)\n", r.From, r.ID, r.Taken.ID)
			case r.OriginMissing:
				fmt.Fprintf(out, "%s is no longer in the backend: nothing to close\n", r.Taken.FromWork)
			default:
				fmt.Fprintf(out, "%s is %s: left as it is\n", r.Taken.FromWork, r.OriginState)
			}
			if len(r.DroppedRefs) > 0 {
				fmt.Fprintf(out, "refs no longer in the backend, left off %s: %s\n", r.ID, strings.Join(r.DroppedRefs, ","))
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

// handoffNow is the clock of the relative ages ("pending 3h"); tests pin it.
var handoffNow = time.Now

// takenWindow is how far back the default list shows taken handoffs.
const takenWindow = 14 * 24 * time.Hour

func newHandoffListCmd(dir *string) *cobra.Command {
	var state string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list [--state pending|taken|superseded|cancelled|all] [--json]",
		Short: "List the handoffs: pending ones first, then those taken in the last 14 days",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if state != "" && state != "all" && !slices.Contains([]domain.HandoffState{domain.HandoffPending, domain.HandoffTaken, domain.HandoffSuperseded, domain.HandoffCancelled}, domain.HandoffState(state)) {
				return fmt.Errorf("--state %q: want pending, taken, superseded, cancelled or all", state)
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			all, err := store.LoadAllHandoffs(ctx.repoDir)
			if err != nil {
				return err
			}
			now := handoffNow()
			var pending, rest []store.Handoff
			for _, h := range all {
				switch {
				case state == "" && h.State == domain.HandoffTaken && h.TakenAt != nil && now.Sub(*h.TakenAt) > takenWindow:
				case state == "" && h.State != domain.HandoffPending && h.State != domain.HandoffTaken:
				case state != "" && state != "all" && string(h.State) != state:
				case h.State == domain.HandoffPending:
					pending = append(pending, h)
				default:
					rest = append(rest, h)
				}
			}
			slices.SortStableFunc(pending, func(a, b store.Handoff) int { return a.CreatedAt.Compare(b.CreatedAt) })
			list := append(pending, rest...)
			out := cmd.OutOrStdout()
			if asJSON {
				if list == nil {
					list = []store.Handoff{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			if len(list) == 0 {
				fmt.Fprintln(out, "no handoffs")
				return nil
			}
			titles, err := workTitles(ctx.repoDir)
			if err != nil {
				return err
			}
			for _, h := range list {
				fmt.Fprintln(out, h.Line(titles[h.FromWork], now))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "only this state: pending|taken|superseded|cancelled|all (default: pending + taken in the last 14 days)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output, every field")
	return cmd
}

// workTitles maps each work id to its title.
func workTitles(repoDir string) (map[string]string, error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, w := range works {
		m[w.ID] = w.Title
	}
	return m, nil
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
