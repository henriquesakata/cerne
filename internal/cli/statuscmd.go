package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// statusStateShort is the compact label for a feature state, used in the
// EPICS child list.
var statusStateShort = map[domain.FeatureState]string{
	domain.StateCandidate:     "candidate",
	domain.StateApproved:      "approved",
	domain.StateInDevelopment: "in development",
	domain.StateImplemented:   "implemented",
	domain.StateDone:          "done",
	domain.StateSuperseded:    "superseded",
	domain.StateRejected:      "rejected",
}

// statusGroup pairs a feature state with its table header, in the
// fixed display order of `cerne status`.
type statusGroup struct {
	state  domain.FeatureState
	header string
}

var statusDefaultGroups = []statusGroup{
	{domain.StateInDevelopment, "IN DEVELOPMENT"},
	{domain.StateApproved, "APPROVED"},
	{domain.StateCandidate, "CANDIDATES"},
	{domain.StateImplemented, "IMPLEMENTED"},
	// done (F-0131) is accepted history: shown by default like implemented
	// (the kanban's Done column), hidden by --active with the rest.
	{domain.StateDone, "DONE"},
}

var statusTerminalGroups = []statusGroup{
	{domain.StateSuperseded, "SUPERSEDED"},
	{domain.StateRejected, "REJECTED"},
}

// activeGroups is the lean view (cerne status --active): only the non-terminal
// work an agent is likely picking up. Implemented history is omitted — it is the
// bulk of a mature board and pure noise for "what should I work on".
var activeGroups = []statusGroup{
	{domain.StateInDevelopment, "IN DEVELOPMENT"},
	{domain.StateApproved, "APPROVED"},
	{domain.StateCandidate, "CANDIDATES"},
}

func newStatusCmd() *cobra.Command {
	var dir string
	var all, active, asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show features grouped by state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, dir, all, active, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&all, "all", false, "include superseded and rejected features")
	cmd.Flags().BoolVar(&active, "active", false, "show only active work (hides IMPLEMENTED and terminals)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output (all features)")
	return cmd
}

func runStatus(cmd *cobra.Command, dir string, all, active, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}
	historicals, err := store.LoadAllHistoricals(ctx.repoDir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	gauntlet := "on"
	if !ctx.cfg.Gates.GauntletEnabled() {
		gauntlet = "off"
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Features    []store.Feature    `json:"features"`
			Historicals []store.Historical `json:"historicals"`
			Gauntlet    string             `json:"gauntlet"` // "on" | "off" (light mode, F-0364)
		}{features, historicals, gauntlet})
	}
	// Light mode is a choice the reader must see (F-0364): one line, only
	// when the Gauntlet is off, so a project with it on reads as before —
	// and last, so a parser of the first line reads what it read before.
	if gauntlet == "off" {
		defer fmt.Fprintln(out, `Gauntlet: off (light mode) — turn it on in the Settings screen of cerne ui, or run: cerne settings set gates.gauntlet on --scope project`)
	}
	if active {
		printChannelLead(out, ctx.cfg.ProjectID)
	}

	if len(features) == 0 && !(all && len(historicals) > 0) {
		fmt.Fprintln(out, "No features yet. Create the first one with 'cerne feature new'.")
		return nil
	}

	// --active is the most restrictive view and wins over --all.
	groups := statusDefaultGroups
	switch {
	case active:
		groups = activeGroups
	case all:
		groups = append(append([]statusGroup{}, statusDefaultGroups...), statusTerminalGroups...)
	}

	type renderGroup struct {
		header   string
		features []store.Feature
	}
	var shown []renderGroup
	total, width := 0, 0
	for _, g := range groups {
		var fs []store.Feature
		for _, f := range features {
			if f.State == g.state {
				fs = append(fs, f)
			}
		}
		if len(fs) == 0 {
			continue
		}
		for _, f := range fs {
			if n := utf8.RuneCountInString(f.Module); n > width {
				width = n
			}
		}
		total += len(fs)
		shown = append(shown, renderGroup{g.header, fs})
	}

	for _, g := range shown {
		fmt.Fprintf(out, "%s (%d)\n", g.header, len(g.features))
		for _, f := range g.features {
			fmt.Fprintln(out, statusLine(f, width))
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "Total: %d feature(s)\n", total)

	renderEpics(out, features)

	// Historical entities are terminal by definition; they only show under
	// --all, in their own section after the feature totals.
	if all && !active && len(historicals) > 0 {
		fmt.Fprintf(out, "\nHISTORICALS (%d)\n", len(historicals))
		for _, h := range historicals {
			line := "  " + h.ID + "  " + h.Name
			if h.SupersededBy != "" {
				line += " → " + h.SupersededBy
			}
			fmt.Fprintln(out, line)
		}
	}
	return nil
}

// statusLine renders one table row: ID, module padded to width, title,
// plus "→ <id>" on superseded features. Columns separated by two spaces;
// the module column is omitted entirely when no shown feature has one.
func statusLine(f store.Feature, width int) string {
	var b strings.Builder
	b.WriteString("  " + f.ID + "  ")
	if width > 0 {
		b.WriteString(f.Module)
		b.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(f.Module)+2))
	}
	b.WriteString(f.Title)
	if f.State == domain.StateSuperseded && f.SupersededBy != "" {
		b.WriteString(" → " + f.SupersededBy)
	}
	return b.String()
}

// renderEpics prints the EPICS section: every feature that has at least one
// live child (not superseded/rejected), with its implemented/total progress and
// the live children listed underneath. Epics are sorted by ID; a child pointing
// at a missing parent is left to the lint, not shown here. No output when there
// are no epics.
func renderEpics(out io.Writer, features []store.Feature) {
	byID := make(map[string]store.Feature, len(features))
	for _, f := range features {
		byID[f.ID] = f
	}
	childrenByParent := store.ChildrenByParent(features)
	epicIDs := make([]string, 0, len(childrenByParent))
	for id := range childrenByParent {
		epicIDs = append(epicIDs, id)
	}
	sort.Strings(epicIDs)

	type epicRow struct {
		epic     store.Feature
		children []store.Feature
	}
	var rows []epicRow
	for _, id := range epicIDs {
		epic, ok := byID[id]
		if !ok {
			continue // dangling parent: a lint concern
		}
		var live []store.Feature
		for _, c := range childrenByParent[id] {
			if c.State == domain.StateSuperseded || c.State == domain.StateRejected {
				continue
			}
			live = append(live, c)
		}
		if len(live) == 0 {
			continue
		}
		rows = append(rows, epicRow{epic, live})
	}
	if len(rows) == 0 {
		return
	}

	fmt.Fprintf(out, "\nEPICS (%d)\n", len(rows))
	for _, r := range rows {
		done, total := store.EpicProgress(r.children)
		fmt.Fprintf(out, "  %s  %s — %d/%d implemented\n", r.epic.ID, r.epic.Title, done, total)
		for _, c := range r.children {
			fmt.Fprintf(out, "    %s  [%s]  %s\n", c.ID, statusStateShort[c.State], c.Title)
		}
	}
}
