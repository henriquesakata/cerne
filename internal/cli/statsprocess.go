package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/stats"
)

func runProcessStats(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	r, err := app.ProcessPerf(ctx.repoDir, time.Now().UTC())
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	renderProcess(out, r)
	return nil
}

func renderProcess(out io.Writer, r stats.ProcessReport) {
	fmt.Fprintln(out, "Process performance — weighted tokens per delivered feature")
	fmt.Fprintln(out, "  cost = input + 1.25×cache_creation + 0.1×cache_read + 5×output, from each feature's stamped cost anchor (F-0309);")
	fmt.Fprintln(out, "  only sessions bound to the feature count — spend outside any binding is not in this panel.")
	fmt.Fprintln(out)
	if r.Reason != "" {
		fmt.Fprintln(out, r.Reason)
	} else {
		fmt.Fprintln(out, "Weekly median (ISO week, UTC):")
		for _, w := range r.Weeks {
			fmt.Fprintf(out, "  %s  %s  n=%d\n", w.Week, tokens(w.Median), w.N)
		}
	}
	fmt.Fprintln(out)
	if len(r.Markers) == 0 {
		fmt.Fprintln(out, "No markers: no decision has facet process and component metric.")
	} else {
		fmt.Fprintln(out, "Markers (decisions with facet process + component metric):")
		for _, m := range r.Markers {
			fmt.Fprintf(out, "  %s  %s  %s\n", m.ID, m.At.UTC().Format(time.DateOnly), m.Title)
			renderSide(out, "before", m.Before)
			renderSide(out, "after ", m.After)
			if m.Cost != "" {
				fmt.Fprintf(out, "    guards: returned %s · escaped %s\n", m.Returned, m.Escaped)
			}
			fmt.Fprintf(out, "    verdict: %s — %s\n", m.Verdict, m.Reason)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Excluded: %d delivered without a stamped cost · %d without a Gauntlet record (out of the returns guard only)\n",
		r.ExcludedNoCost, r.ExcludedNoGauntlet)
}

func renderSide(out io.Writer, label string, s stats.Side) {
	fmt.Fprintf(out, "    %s [%s → %s)  n=%d", label, s.From.UTC().Format(time.DateOnly), s.To.UTC().Format(time.DateOnly), s.N)
	if s.N > 0 {
		fmt.Fprintf(out, "  median %s  p25 %s  p75 %s · raw medians: input %.0f · cache_creation %.0f · cache_read %.0f · output %.0f"+
			" · returned %.0f%% of %d with Gauntlet · escaped %.0f%%",
			tokens(s.Median), tokens(s.P25), tokens(s.P75), s.RawMedian.Input, s.RawMedian.CacheCreation,
			s.RawMedian.CacheRead, s.RawMedian.Output, s.ReturnedShare*100, s.WithGauntlet, s.EscapedShare*100)
	}
	if s.CutBy != "" {
		fmt.Fprintf(out, "  (window cut at %s)", s.CutBy)
	}
	fmt.Fprintln(out)
}

// tokens renders a weighted token count compactly: 4.4M, 12.3k, 950.
func tokens(v float64) string {
	switch {
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}
