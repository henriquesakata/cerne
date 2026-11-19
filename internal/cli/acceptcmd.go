package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// newFeatureAcceptCmd wires `cerne feature accept` (F-0131 crit. 3, 7, 9): the
// human validation gate that moves an implemented ("Ready") feature to done.
// It is kept apart from newTransitionCmds because it carries the human-gate
// check (crit. 9) and the --all-implemented bulk migration (crit. 7), neither
// of which the other lifecycle actions need.
func newFeatureAcceptCmd(dir *string) *cobra.Command {
	var reason string
	var allImplemented bool
	cmd := &cobra.Command{
		Use:   "accept [id]",
		Short: "Accept an implemented feature (human validation → done)",
		Long: "Moves an implemented (\"Ready\") feature to done: the human validation gate at the end of the " +
			"lifecycle (F-0131). Under the default human gate, an agent actor (CERNE_ACTOR=agent) is refused — " +
			"only a human accepts. --all-implemented migrates every implemented feature at once (requires --reason).",
		Example: "  cerne feature accept F-0001 --reason \"validated in staging\"\n" +
			"  cerne feature accept --all-implemented --reason \"legacy migration\"",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureAccept(cmd, *dir, args, reason, allImplemented)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "optional acceptance note (required with --all-implemented)")
	cmd.Flags().BoolVar(&allImplemented, "all-implemented", false, "accept every feature currently implemented (legacy bulk migration; requires --reason)")
	return verbs.MarkLifecycle(cmd)
}

func runFeatureAccept(cmd *cobra.Command, dir string, args []string, reason string, allImplemented bool) error {
	id := ""
	if len(args) == 1 {
		id = domain.CanonicalID(args[0])
	}
	if allImplemented && id != "" {
		return fmt.Errorf("--all-implemented and an <id> are mutually exclusive")
	}
	if !allImplemented && id == "" {
		return fmt.Errorf("provide an <id> or use --all-implemented")
	}
	if allImplemented && reason == "" {
		return fmt.Errorf("--reason is required with --all-implemented")
	}

	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if err := app.CheckHumanGate(ctx.cfg.Gates.AcceptGate()); err != nil {
		return err
	}

	if allImplemented {
		return runFeatureAcceptAllImplemented(cmd, ctx, reason)
	}

	// The delivery commit should be on the default branch (F-0355): say so
	// before accepting, never block on it.
	if f, err := store.LoadFeature(ctx.repoDir, id); err == nil {
		if w := app.DeliveryBranchWarning(ctx.cfg, ctx.projDir, f); w != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), w)
		}
	}
	out, err := app.TransitionFeature(ctx.repoDir, ctx.projDir, id, domain.ActionAccept, "", reason, nil)
	if err != nil {
		return err
	}
	printWarnings(cmd.ErrOrStderr(), out.Warnings)
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	printMovedPendings(cmd.OutOrStdout(), out.Pendings)
	printMovedPendings(cmd.OutOrStdout(), out.Ideas)
	printMovedPendings(cmd.OutOrStdout(), out.Cascade) // the tasks it accepted along (T-0007)
	if cost.Unstamped(filepath.Dir(ctx.repoDir), ctx.repoDir, id) {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s's token cost was never stamped: its harvested cost did not reach the result. Run 'cerne cost <id> --stamp' after 'feature complete', before the accept (F-0309).\n", id)
	}
	return nil
}

// runFeatureAcceptAllImplemented is the opt-in legacy migration (crit. 7): it
// iterates every feature currently implemented and accepts each in turn,
// skipping any other state. Never run automatically — the user invokes it.
func runFeatureAcceptAllImplemented(cmd *cobra.Command, ctx projCtx, reason string) error {
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}
	var accepted int
	for _, f := range features {
		if f.State != domain.StateImplemented {
			continue
		}
		// A parent accepted earlier in the loop may have taken this task along
		// (T-0007): read the state again rather than accept it twice.
		if cur, err := store.LoadFeature(ctx.repoDir, f.ID); err == nil && cur.State == domain.StateDone {
			accepted++
			continue
		}
		out, err := app.TransitionFeature(ctx.repoDir, ctx.projDir, f.ID, domain.ActionAccept, "", reason, nil)
		if err != nil {
			return fmt.Errorf("accepting %s: %w", f.ID, err)
		}
		printWarnings(cmd.ErrOrStderr(), out.Warnings)
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
		printMovedPendings(cmd.OutOrStdout(), out.Pendings)
		printMovedPendings(cmd.OutOrStdout(), out.Ideas)
		printMovedPendings(cmd.OutOrStdout(), out.Cascade)
		accepted++
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%d feature(s) accepted.\n", accepted)
	return nil
}

// printWarnings writes a transition's non-blocking advisories (the mirror's
// failed tracker round, F-0449), one per line.
func printWarnings(w io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}
