package cli

import "github.com/spf13/cobra"

func newSyncCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile with the backend and send (pull with merge + push) — always a path forward",
		Long: "Syncs the project with the backend without deadlock: first it merges the remote advance into " +
			".docs/ while preserving your unpushed edits (like 'cerne pull'), then it sends your edits " +
			"reconciling per document (like 'cerne push'). A real same-document conflict " +
			"stops before sending, non-destructively, for you to resolve.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

// runSync is the no-deadlock forward path: a non-destructive pull that merges
// the remote advance while keeping unsent edits, then a per-document push that
// auto-rebases and retries. A same-document conflict surfaces in the pull step
// and stops sync before the push (nothing lost), so there is never a state where
// push says "pull" and pull says "push".
func runSync(cmd *cobra.Command, dir string) error {
	// scope "" → default_scope from .cernerc; okf default from config (sync has
	// no --okf flag, so it is never "Changed"); force false → never discard.
	if err := runPull(cmd, dir, "", false, false); err != nil {
		return err
	}
	return runPush(cmd, dir, false)
}
