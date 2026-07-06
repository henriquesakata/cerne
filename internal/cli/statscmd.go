package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/materialize"
)

// statsKindOrder is the fixed display order of the per-kind table rows.
var statsKindOrder = []string{"feature", "decision", "reference", "runbook", "note"}

func newStatsCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Context savings: full corpus vs the current .docs/ projection",
		Long: "Measures how much documentation the backend holds (every entity, every state) against " +
			"what the current .docs/ projection actually delivers to an agent — per kind and in total, " +
			"as counts, bytes and an estimated token figure (bytes/4). The difference is the context " +
			"the agent never has to read. --json emits the full object for machine consumption.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(cmd, dir, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runStats(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	s, err := materialize.ComputeStats(ctx.repoDir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	renderStats(out, s)
	return nil
}

// renderStats prints the compact savings table: per kind and total, the full
// corpus vs the current projection, then the saved line with the percentage.
func renderStats(out io.Writer, s materialize.Stats) {
	fmt.Fprintln(out, "Context savings — full corpus vs current .docs/ projection")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%-10s  %28s    %28s\n", "", "CORPUS (all states)", "CURRENT (projected)")
	fmt.Fprintf(out, "%-10s  %6s  %9s  %9s    %6s  %9s  %9s\n",
		"KIND", "COUNT", "BYTES", "~TOKENS", "COUNT", "BYTES", "~TOKENS")
	for _, kind := range statsKindOrder {
		ks := s.Kinds[kind]
		fmt.Fprintf(out, "%-10s  %6d  %9d  %9d    %6d  %9d  %9d\n",
			kind, ks.Count, ks.Bytes, ks.Bytes/4, ks.CurrentCount, ks.CurrentBytes, ks.CurrentBytes/4)
	}
	fmt.Fprintf(out, "%-10s  %6d  %9d  %9d    %6d  %9d  %9d\n",
		"TOTAL", s.TotalCount, s.TotalBytes, s.TotalTokensEst, s.CurrentCount, s.CurrentBytes, s.CurrentTokensEst)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Saved: %d bytes (~%d tokens) — %.1f%% of the corpus stays out of the agent's context\n",
		s.SavedBytes, s.SavedTokensEst, s.SavedPct)
	fmt.Fprintln(out, "Token figures are a bytes/4 estimate.")
}
