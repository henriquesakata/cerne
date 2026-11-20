package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
)

// newContextCmd is the task package (E-0005): what a task needs to start, in
// one read, within a budget.
func newContextCmd() *cobra.Command {
	var dir string
	var asJSON bool
	var budget int
	cmd := &cobra.Command{
		Use:   "context <id>",
		Short: "The task package: a task's stage document, its epic and what informs it, within a token budget",
		Long: "Prints what a feature, epic, task or bug needs to start: its document of the stage (the plan in " +
			"development) whole, its epic, and its neighbourhood in four rings — (a) what its documents and its " +
			"epic's link by [[id]] or what informs them; (b) active notes whose edit: trigger or cited file matches a " +
			"path its spec or plan cites; (c) active notes sharing a component, the least used first; (d) up to five " +
			"active notes the search index ranks for its title and problem. The active notes and decisions of ring " +
			"(a), which the author linked, come with their body; every other item is an id, a title and a one-line " +
			"gist — read a body with 'cerne show <id>'. It closes with the counts of what shares a component and " +
			"stays on demand. Past --budget (estimated tokens) the list drops to id and title, the items of rings " +
			"(d), (c) and (b) leave first, then the ring (a) bodies, the longest first, then the ring (a) items; " +
			"the document is never cut. Reads the local clone and index as they are: " +
			"no network, no model. --json for structured output.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The cached clone as it is (resolvePaths): no fetch, so no network
			// and the time budget of criterion 15 (E-0005).
			ctx, err := resolvePaths(dir)
			if err != nil {
				return err
			}
			p, err := app.TaskContext(ctx.repoDir, args[0], budget)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(p)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), p.Text())
			return err
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(&budget, "budget", app.DefaultContextBudget, "ceiling in estimated tokens (~4 bytes each)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}
