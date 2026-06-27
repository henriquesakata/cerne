package cli

import "github.com/spf13/cobra"

func newSyncCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconciliar com o backend e enviar (pull com merge + push) — sempre um caminho à frente",
		Long: "Sincroniza o projeto com o backend sem deadlock: primeiro funde o avanço remoto em " +
			".docs/ preservando suas edições não-enviadas (como 'cerne pull'), depois envia suas edições " +
			"reconciliando por documento (como 'cerne push'). Um conflito real de mesmo-documento " +
			"interrompe antes do envio, de forma não-destrutiva, para você resolver.",
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
