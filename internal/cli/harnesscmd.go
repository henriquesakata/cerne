package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/harness"
)

func newHarnessCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "Report the project's effective agent harness (read-only inventory)",
		Long:  "Lists every piece of the effective agent harness — cerne-managed hooks and skills (with drift against this build), MCP servers, marketplace plugins, context files, provider parity — with the origin layer and the repair command when one applies. Same payload as GET /api/harness (F-0251).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHarness(cmd, dir, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the inventory as JSON")
	return cmd
}

func runHarness(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	inv := harness.Inspect(harness.Options{ProjDir: ctx.projDir, RepoDir: ctx.repoDir, Home: home})

	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(inv)
	}

	fmt.Fprintf(out, "cerne %s — harness inventory\n", inv.Version)
	for _, c := range inv.Classes {
		if len(c.Items) == 0 {
			continue
		}
		fmt.Fprintf(out, "\n%s\n", c.Name)
		w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		for _, it := range c.Items {
			detail := it.Detail
			if it.Fix != "" {
				if detail != "" {
					detail += " — "
				}
				detail += "fix: " + it.Fix
			}
			outside := ""
			if it.OutsideRepo {
				outside = " (outside repo)"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s%s\t%s\n", it.Name, it.State, it.Origin, outside, detail)
		}
		w.Flush()
	}
	var degraded []harness.Layer
	for _, l := range inv.Layers {
		if !l.Readable && l.Error != "not present" {
			degraded = append(degraded, l)
		}
	}
	if len(degraded) > 0 {
		fmt.Fprintln(out, "\nlayers with problems")
		for _, l := range degraded {
			fmt.Fprintf(out, "  %s: %s\n", l.Path, l.Error)
		}
	}
	return nil
}
