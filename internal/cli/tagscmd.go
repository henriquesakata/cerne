package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// newTagsCmd is the tag vocabulary group (E-0005 criterion 21): run bare, it
// lists the components in use; its subcommands (F-0482: rename) inherit --dir.
func newTagsCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "List the components in use, with the count of entities per kind",
		Long: "Lists every component in the backend — on features, notes, decisions, references, runbooks, " +
			"pendings and ideas, in any state — with how many entities of each kind carry it. Read it before " +
			"tagging: a new component of the same root as an existing one, or a few characters from it, is " +
			"refused unless --force. --json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTags(cmd, dir, asJSON)
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.AddCommand(verbs.MarkChat(newTagsRenameCmd(&dir)))
	return cmd
}

func runTags(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	use, err := app.ComponentUsage(ctx.repoDir)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Components map[string]map[string]int `json:"components"`
		}{use})
	}
	if len(use) == 0 {
		fmt.Fprintln(w, "No components in use. Tag with --component on new/retag; pick short, shared subsystem names.")
		return nil
	}
	fmt.Fprintf(w, "Components in use (%d), entities per kind:\n", len(use))
	for _, c := range slices.Sorted(maps.Keys(use)) {
		total, kinds := 0, make([]string, 0, len(use[c]))
		for _, k := range slices.Sorted(maps.Keys(use[c])) {
			total += use[c][k]
			kinds = append(kinds, fmt.Sprintf("%s %d", k, use[c][k]))
		}
		fmt.Fprintf(w, "  %s (%d): %s\n", c, total, strings.Join(kinds, ", "))
	}
	fmt.Fprintln(w, "Reuse one of these; a new name near an existing one is refused unless --force.")
	return nil
}
