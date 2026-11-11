package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/stats"
	"github.com/henriquesakata/cerne/internal/store"
)

func newStatsCmd() *cobra.Command {
	var dir, since string
	var asJSON, process bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Flow metrics (lead time, cycle time, throughput) from the event log",
		Long: "Aggregates lead time (created -> implemented) and cycle time (started -> implemented) per " +
			"completed feature, plus median/p90 and monthly throughput — computed purely from the event " +
			"log (read-only, no writes). --since restricts the window to features completed at or after " +
			"that date/duration (e.g. \"2026-01-01\" or \"90d\"); default is all time. --json for structured output. " +
			"--process prints the process panel instead (F-0321): the weekly median of weighted tokens per delivered " +
			"feature, from the stamped cost anchors, and each change marker (a decision with facet process + component " +
			"metric) judged before/after with its reason and quality guard.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if process {
				return runProcessStats(cmd, dir, asJSON)
			}
			return runFlowStats(cmd, dir, since, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&since, "since", "", `restrict to features completed since this date (YYYY-MM-DD) or duration (e.g. "90d")`)
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().BoolVar(&process, "process", false, "process performance: weighted tokens per delivered feature, quality guard and change markers (F-0321)")
	cmd.MarkFlagsMutuallyExclusive("process", "since")
	return cmd
}

func runFlowStats(cmd *cobra.Command, dir, since string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	sinceTime, err := parseSince(since)
	if err != nil {
		return err
	}
	events, err := store.ReadEvents(ctx.repoDir)
	if err != nil {
		return err
	}
	m, err := stats.Compute(events, sinceTime)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}
	renderFlowStats(out, m)
	return nil
}

// parseSince accepts a "YYYY-MM-DD" date or a Go duration suffixed with "d"
// (days, since time.ParseDuration has no day unit) meaning "that long ago
// from now". Empty input means no restriction (zero time.Time).
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Now().UTC().AddDate(0, 0, -days), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: want YYYY-MM-DD or a duration like \"90d\"", s)
}

func renderFlowStats(out io.Writer, m stats.Metrics) {
	fmt.Fprintln(out, "Flow metrics — from the event log")
	fmt.Fprintln(out)
	if len(m.Features) == 0 {
		fmt.Fprintln(out, "No completed features in this window.")
	} else {
		fmt.Fprintf(out, "%-10s  %10s  %10s\n", "FEATURE", "Lead", "Cycle")
		for _, f := range m.Features {
			fmt.Fprintf(out, "%-10s  %10s  %10s\n", f.ID, f.Lead.Round(time.Hour), f.Cycle.Round(time.Hour))
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Lead time  — median: %s, p90: %s\n", m.LeadMedian.Round(time.Hour), m.LeadP90.Round(time.Hour))
		fmt.Fprintf(out, "Cycle time — median: %s, p90: %s\n", m.CycleMedian.Round(time.Hour), m.CycleP90.Round(time.Hour))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Throughput (features completed per month):")
	if len(m.ThroughputByMonth) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	for _, month := range sortedMonths(m.ThroughputByMonth) {
		fmt.Fprintf(out, "  %s  %d\n", month, m.ThroughputByMonth[month])
	}
	if m.Excluded > 0 {
		fmt.Fprintf(out, "\n%d feature(s) excluded: missing created/started events (e.g. bulk-imported)\n", m.Excluded)
	}
}

func sortedMonths(byMonth map[string]int) []string {
	months := make([]string, 0, len(byMonth))
	for month := range byMonth {
		months = append(months, month)
	}
	sort.Strings(months)
	return months
}
