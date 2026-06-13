package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// statusGroup pairs a feature state with its pt-BR table header, in the
// fixed display order of `cerne status`.
type statusGroup struct {
	state  domain.FeatureState
	header string
}

var statusDefaultGroups = []statusGroup{
	{domain.StateInDevelopment, "EM DESENVOLVIMENTO"},
	{domain.StateApproved, "APROVADAS"},
	{domain.StateCandidate, "CANDIDATAS"},
	{domain.StateImplemented, "IMPLEMENTADAS"},
}

var statusTerminalGroups = []statusGroup{
	{domain.StateSuperseded, "SUBSTITUÍDAS"},
	{domain.StateRejected, "REJEITADAS"},
}

func newStatusCmd() *cobra.Command {
	var dir string
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show features grouped by state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, dir, all, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&all, "all", false, "include superseded and rejected features")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output (all features)")
	return cmd
}

func runStatus(cmd *cobra.Command, dir string, all, asJSON bool) error {
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

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Features    []store.Feature    `json:"features"`
			Historicals []store.Historical `json:"historicals"`
		}{features, historicals})
	}

	if len(features) == 0 && !(all && len(historicals) > 0) {
		fmt.Fprintln(out, "Nenhuma feature ainda. Crie a primeira com 'cerne feature new'.")
		return nil
	}

	groups := statusDefaultGroups
	if all {
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

	// Historical entities are terminal by definition; they only show under
	// --all, in their own section after the feature totals.
	if all && len(historicals) > 0 {
		fmt.Fprintf(out, "\nHISTÓRICAS (%d)\n", len(historicals))
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
