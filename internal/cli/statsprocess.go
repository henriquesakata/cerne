package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/stats"
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
	fmt.Fprintln(out, "  only sessions bound to the feature count — spend outside any binding is not in this panel; each side's coverage")
	fmt.Fprintln(out, "  says how much of its bound sessions it sees. The level is not comparable to a series built from all transcripts.")
	fmt.Fprintln(out)
	if r.Reason != "" {
		fmt.Fprintln(out, r.Reason)
	} else {
		fmt.Fprintln(out, "Weekly median (ISO week, UTC):")
		for _, w := range r.Weeks {
			fmt.Fprintf(out, "  %s  %s  n=%d of %d delivered\n", w.Week, tokens(w.Median), w.N, w.Delivered)
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
			fmt.Fprintf(out, "    verdict: %s%s — %s\n", m.Verdict, unguarded(m), m.Reason)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Excluded: %d delivered without a stamped cost · %d epic(s), features with children whose own cost is orchestration only"+
		" · %d without a Gauntlet record (out of the returns guard only)\n", r.ExcludedNoCost, r.ExcludedEpic, r.ExcludedNoGauntlet)
}

func renderSide(out io.Writer, label string, s stats.Side) {
	fmt.Fprintf(out, "    %s [%s → %s)  n=%d of %d delivered", label, s.From.UTC().Format(time.DateOnly), s.To.UTC().Format(time.DateOnly), s.N, s.Delivered)
	var excl []string
	if s.ExcludedNoCost > 0 {
		excl = append(excl, fmt.Sprintf("%d without a stamped cost", s.ExcludedNoCost))
	}
	if s.ExcludedEpic > 0 {
		excl = append(excl, fmt.Sprintf("%d epic(s)", s.ExcludedEpic))
	}
	if s.ExcludedNoGauntlet > 0 {
		excl = append(excl, fmt.Sprintf("%d without a Gauntlet record", s.ExcludedNoGauntlet))
	}
	if len(excl) > 0 {
		fmt.Fprintf(out, " (%s)", strings.Join(excl, ", "))
	}
	if s.N > 0 {
		fmt.Fprintf(out, "  median %s  p25 %s  p75 %s · raw medians: input %.0f · cache_creation %.0f · cache_read %.0f · output %.0f"+
			" · unattributed median %s · coverage %.0f%% · returned %.0f%% of %d with Gauntlet · escaped %s",
			tokens(s.Median), tokens(s.P25), tokens(s.P75), s.RawMedian.Input, s.RawMedian.CacheCreation,
			s.RawMedian.CacheRead, s.RawMedian.Output, tokens(s.UnattributedMedian), s.Coverage*100,
			s.ReturnedShare*100, s.WithGauntlet, escapedShare(s))
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

// unguarded qualifies a melhorou whose guards were not both evaluated (C9):
// it must not read like a guarded one.
func unguarded(m stats.MarkerReport) string {
	if m.Verdict != "melhorou" || m.Guarded {
		return ""
	}
	var lack []string
	if m.Returned == "inconclusivo" {
		lack = append(lack, "returned inconclusive")
	}
	if m.Escaped == "pending" {
		lack = append(lack, "escaped pending")
	}
	return " (unguarded: " + strings.Join(lack, ", ") + ")"
}

// escapedShare is a side's escaped share, or "pending" while its deliveries
// are too young to have revealed a defect (C10).
func escapedShare(s stats.Side) string {
	if s.EscapedShare == nil {
		return "pending"
	}
	return fmt.Sprintf("%.0f%%", *s.EscapedShare*100)
}
