package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/verbs"
)

const (
	// learningsPromptItems caps the items the complete prompt shows (F-0401).
	learningsPromptItems = 10
	learningsItemChars   = 160
)

func newFeatureLearningsCmd(dir *string) *cobra.Command {
	var (
		featureID, since, skip, reason string
		limit                          int
		all, asJSON                    bool
	)
	cmd := &cobra.Command{
		Use:   "learnings",
		Short: "List the learnings of concluded features that nobody triaged yet (--skip F-NNNN records that none is durable)",
		Long: "Lists, read-only, the implemented or done features whose result has learnings items " +
			"(\"Aprendizados e desvios\") and that are not triaged yet, newest first, each item with the active " +
			"notes it may complement or replace (found by a shared file path, then a shared component). A feature " +
			"is triaged by an active note that informs it and is newer than its conclusion, or by --skip. " +
			"Epics, rejected and superseded features are left out. For each durable item record a note " +
			"('cerne note new --informs F-NNNN'); complement an existing one with 'cerne note errata' or " +
			"replace it with 'cerne note substituir'. --skip F-NNNN [--reason \"...\"] records that nothing in " +
			"that feature's learnings is durable.",
		Example: "  cerne feature learnings                       # the 10 newest pending features\n" +
			"  cerne feature learnings --all --json          # everything pending, machine-readable\n" +
			"  cerne feature learnings --feature F-0123      # one feature\n" +
			"  cerne feature learnings --since 2026-11-01    # concluded since a date\n" +
			"  cerne feature learnings --skip F-0123 --reason \"only provenance\"",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureLearnings(cmd, *dir, featureID, since, skip, reason, limit, all, asJSON)
		},
	}
	cmd.Flags().StringVar(&featureID, "feature", "", "list only this feature (F-NNNN)")
	cmd.Flags().StringVar(&since, "since", "", "only features concluded on or after this date (YYYY-MM-DD)")
	cmd.Flags().IntVar(&limit, "limit", 10, "how many features to list")
	cmd.Flags().BoolVar(&all, "all", false, "list every pending feature (ignores --limit)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output (items carry related_notes)")
	cmd.Flags().StringVar(&skip, "skip", "", "record that nothing in this feature's learnings is durable (F-NNNN)")
	cmd.Flags().StringVar(&reason, "reason", "", "why nothing is durable (with --skip)")
	return verbs.MarkLocal(cmd)
}

func runFeatureLearnings(cmd *cobra.Command, dir, featureID, since, skip, reason string, limit int, all, asJSON bool) error {
	if reason != "" && skip == "" {
		return errors.New("--reason only applies with --skip")
	}
	if limit < 1 {
		return fmt.Errorf("--limit must be at least 1, got %d", limit)
	}
	opts := app.LearningsOpts{Feature: strings.ToUpper(strings.TrimSpace(featureID)), Limit: limit, All: all}
	if since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err != nil {
			return fmt.Errorf("--since wants a date as YYYY-MM-DD, got %q", since)
		}
		opts.Since = t
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if skip != "" {
		id := strings.ToUpper(strings.TrimSpace(skip))
		if err := app.SkipLearnings(ctx.repoDir, ctx.projDir, id, reason, time.Now()); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: learnings marked as triaged, nothing durable.\n", id)
		return nil
	}
	rep, err := app.Learnings(ctx.repoDir, opts)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	for _, f := range rep.Features {
		fmt.Fprintf(out, "%s  %s  (concluded %s, %d item(s))\n", f.ID, f.Title, f.CompletedAt.Format("2006-01-02"), len(f.Items))
		printLearningItems(out, f.Items, len(f.Items))
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "%d feature(s), %d item(s) pending triage", rep.TotalFeatures, rep.TotalItems)
	if len(rep.Features) < rep.TotalFeatures {
		fmt.Fprintf(out, " (showing %d; --all lists the rest)", len(rep.Features))
	}
	fmt.Fprintln(out)
	if len(rep.Features) > 0 {
		fmt.Fprintln(out, learningsTail("F-NNNN", rep.Features...))
	}
	return nil
}

// printLearningItems prints up to max items cut to learningsItemChars, each
// followed by its related notes.
func printLearningItems(w io.Writer, items []app.LearningItem, max int) {
	for i, it := range items {
		if i == max {
			fmt.Fprintf(w, "  (+%d more)\n", len(items)-max)
			break
		}
		fmt.Fprintf(w, "  %d. %s\n", i+1, cutRunes(strings.Join(strings.Fields(it.Text), " "), learningsItemChars))
		for _, n := range it.RelatedNotes {
			fmt.Fprintf(w, "     related (%s): %s — %s\n", n.Via, n.ID, strings.Join(strings.Fields(n.Title), " "))
		}
	}
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// learningsTail is the closing guidance: the record-or-skip line always, and
// the complement and replace paths when some item has a related note.
func learningsTail(id string, feats ...app.LearningFeature) string {
	lines := []string{fmt.Sprintf("durable for the next agent? record it: cerne note new \"<title>\" --informs %s --component <topic> (add golden-path for a procedure); nothing durable: cerne feature learnings --skip %s", id, id)}
	for _, f := range feats {
		for _, it := range f.Items {
			if len(it.RelatedNotes) > 0 {
				n := it.RelatedNotes[0].ID
				return strings.Join(append(lines,
					fmt.Sprintf("complements %s? cerne note errata %s ...", n, n),
					fmt.Sprintf("replaces or contradicts %s? record the new note, then cerne note substituir %s --by <new id> --reason \"...\"", n, n)), "\n")
			}
		}
	}
	return lines[0]
}

// printCompleteLearnings is the read-only prompt after a complete (F-0401):
// silent without items, and silent on any failure — it never decides the exit.
func printCompleteLearnings(w io.Writer, repoDir, id string) {
	rep, err := app.Learnings(repoDir, app.LearningsOpts{Feature: id, All: true})
	if err != nil || len(rep.Features) == 0 {
		return
	}
	f := rep.Features[0]
	fmt.Fprintf(w, "Learnings in the result of %s:\n", id)
	printLearningItems(w, f.Items, learningsPromptItems)
	fmt.Fprintln(w, learningsTail(id, f))
}
