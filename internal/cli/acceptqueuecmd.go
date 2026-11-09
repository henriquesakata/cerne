package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/verbs"
)

// F-0875: `accept queue`. The group never accepts: accept stays the human's
// gate (F-0131), one 'cerne feature accept <id>' per feature.

func newAcceptCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "accept",
		Short: "Read the queue of features waiting for the human's accept",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 { // never fall back to help on a typo: 'accept all' must fail loudly
				return fmt.Errorf("unknown accept subcommand %q (only 'queue'; accepting is 'cerne feature accept <id>')", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkLocal(newAcceptQueueCmd(&dir)))
	return cmd
}

func newAcceptQueueCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "queue [--json]",
		Short: "List implemented features, oldest first, with Gauntlet verdict, production evidence and declared pendings (read-only)",
		Long: "Lists the features in implemented, from the oldest to the newest, each with the Gauntlet verdict, the " +
			"production evidence and the pendings its result.md declares. A field the result does not carry is " +
			"marked as not informed: nothing is inferred. Read-only: it never accepts anything. The batch is the list of " +
			"individual 'cerne feature accept <id>' commands at the end, for the human to run one by one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			rows, err := app.AcceptQueue(ctx.repoDir)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON {
				cmds := []string{}
				for _, r := range rows {
					cmds = append(cmds, r.AcceptCommand)
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Queue          []app.AcceptRow `json:"queue"`
					AcceptCommands []string        `json:"accept_commands"`
				}{rows, cmds})
			}
			if len(rows) == 0 {
				fmt.Fprintln(w, "accept queue: no feature in implemented")
				return nil
			}
			fmt.Fprintf(w, "accept queue: %d implemented, oldest first\n", len(rows))
			for _, r := range rows {
				fmt.Fprint(w, app.FormatAcceptRow(r))
			}
			fmt.Fprintln(w, "\nTo accept, run each one you decide on (one OK per feature):")
			for _, r := range rows {
				fmt.Fprintf(w, "  %s\n", r.AcceptCommand)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}
