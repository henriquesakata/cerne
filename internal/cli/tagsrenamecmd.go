package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
)

// renameTitleRunes caps the title column of `cerne tags rename`.
const renameTitleRunes = 40

// newTagsRenameCmd is `cerne tags rename` (F-0482 criterion 7): one component
// renamed on every entity that carries it, in one write.
func newTagsRenameCmd(dir *string) *cobra.Command {
	var dryRun, force, asJSON bool
	cmd := &cobra.Command{
		Use:   "rename <from> <to>",
		Short: "Rename a component on every entity that carries it, in one write (--dry-run first)",
		Long: "Replaces component <from> by <to> on every feature, task, fix, epic, note, decision, reference, " +
			"runbook, pending and idea that carries it, in any state, closed ones included: one commit, one retag " +
			"event per entity. Run --dry-run first: it lists each entity with its state and tags before and after, " +
			"says whether <to> is new or a merge into a component in use, and writes nothing. A new <to> near an " +
			"existing component is refused unless --force; a <to> already in use is a merge. --json for structured output.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			rep, err := app.RenameComponent(ctx.repoDir, ctx.projDir, args[0], args[1], force, dryRun)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}
			printRename(w, rep, force)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would change and write nothing")
	cmd.Flags().BoolVar(&force, "force", false, forceComponentHelp)
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

// printRename answers what was (or would be) changed, on how many entities,
// and what to check before writing: total, count per kind, one line per
// entity, then new-or-merge and, on a dry run, the command that applies it.
func printRename(w io.Writer, rep app.RenameReport, force bool) {
	n := len(rep.Entities)
	if rep.DryRun {
		fmt.Fprintf(w, "Would rename component %q to %q on %s — nothing written.\n", rep.From, rep.To, entityCount(n))
	} else {
		fmt.Fprintf(w, "Renamed component %q to %q on %s in one commit, one retag event each.\n", rep.From, rep.To, entityCount(n))
	}
	var kinds []string
	count := map[string]int{}
	for _, e := range rep.Entities {
		if count[e.Kind] == 0 {
			kinds = append(kinds, e.Kind)
		}
		count[e.Kind]++
	}
	for i, k := range kinds {
		kinds[i] = fmt.Sprintf("%s %d", k, count[k])
	}
	fmt.Fprintf(w, "By kind: %s\n", strings.Join(kinds, ", "))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range rep.Entities {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s → %s\n", e.ID, e.Kind, e.State, capRunes(e.Title, renameTitleRunes),
			strings.Join(e.Before, ", "), strings.Join(e.After, ", "))
	}
	_ = tw.Flush()
	if rep.Merge {
		fmt.Fprintf(w, "%q is already on %s: this merges %q into it; an entity with both keeps one.\n", rep.To, entityCount(rep.ToCount), rep.From)
	} else {
		fmt.Fprintf(w, "%q is a new component: no entity has it yet.\n", rep.To)
	}
	if rep.DryRun {
		apply := fmt.Sprintf("cerne tags rename %s %s", rep.From, rep.To)
		if force {
			apply += " --force"
		}
		fmt.Fprintf(w, "Apply: %s\n", apply)
	}
}

func entityCount(n int) string {
	if n == 1 {
		return "1 entity"
	}
	return fmt.Sprintf("%d entities", n)
}
