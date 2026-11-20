package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/materialize"
)

// savingsKindOrder is the fixed display order of the per-kind table rows.
var savingsKindOrder = []string{"feature", "decision", "reference", "runbook", "note"}

func newSavingsCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "savings",
		Short: "Context savings: full corpus vs the current .docs/ projection",
		Long: "Measures how much documentation the backend holds (every entity, every state) against " +
			"the entity documents the current .docs/ projection delivers to an agent — per kind and in total, " +
			"as counts, bytes and an estimated token figure (bytes/4). The difference is the context " +
			"the agent never has to read. The generated .docs/CONTEXT.md roster is not counted on either side. " +
			"--json emits the full object for machine consumption.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavings(cmd, dir, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runSavings(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	s, err := materialize.ComputeStats(ctx.repoDir)
	if err != nil {
		return err
	}
	// The map's size (E-0005 criterion 5): 0 when it is not materialized.
	var mapBytes int64
	if fi, err := os.Stat(filepath.Join(ctx.projDir, ".docs", "CONTEXT.md")); err == nil {
		mapBytes = fi.Size()
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			materialize.Stats
			ContextBytes int64 `json:"context_md_bytes"`
		}{s, mapBytes})
	}
	renderSavings(out, s, mapBytes)
	return nil
}

// renderSavings prints the compact savings table: per kind and total, the full
// corpus vs the current projection, then the saved line with the percentage
// and the size of the map (mapBytes 0: not materialized).
func renderSavings(out io.Writer, s materialize.Stats, mapBytes int64) {
	fmt.Fprintln(out, "Corpus reduction — full corpus vs current .docs/ projection")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%-10s  %28s    %28s\n", "", "CORPUS (all states)", "CURRENT (projected)")
	fmt.Fprintf(out, "%-10s  %6s  %9s  %9s    %6s  %9s  %9s\n",
		"KIND", "COUNT", "BYTES", "~TOKENS", "COUNT", "BYTES", "~TOKENS")
	for _, kind := range savingsKindOrder {
		ks := s.Kinds[kind]
		fmt.Fprintf(out, "%-10s  %6d  %9d  %9d    %6d  %9d  %9d\n",
			kind, ks.Count, ks.Bytes, ks.Bytes/4, ks.CurrentCount, ks.CurrentBytes, ks.CurrentBytes/4)
	}
	fmt.Fprintf(out, "%-10s  %6d  %9d  %9d    %6d  %9d  %9d\n",
		"TOTAL", s.TotalCount, s.TotalBytes, s.TotalTokensEst, s.CurrentCount, s.CurrentBytes, s.CurrentTokensEst)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Reduced: %d bytes (~%d tokens) — %.1f%% of the corpus stays in history, outside the folder the agent searches\n",
		s.SavedBytes, s.SavedTokensEst, s.SavedPct)
	if mapBytes > 0 {
		fmt.Fprintf(out, "Map: .docs/CONTEXT.md is %d bytes (~%d tokens), the map a session starts from.\n", mapBytes, mapBytes/4)
	} else {
		fmt.Fprintln(out, "Map: .docs/CONTEXT.md is not materialized yet — run 'cerne pull'.")
	}
	fmt.Fprintln(out, "Token figures are a bytes/4 estimate. The generated .docs/CONTEXT.md is not counted on either side.")
}
