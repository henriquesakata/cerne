package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/store"
)

// briefItem is one entity in the cheap map: id, state and title only — no path,
// no body. The materialized path is reconstructible from the id, so brief omits
// it to stay token-lean (its whole reason to exist beside the path-heavy
// CONTEXT.md): an agent loads the entire map cheaply, then 'cerne show <id>'
// only the few entities it needs.
type briefItem struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Title string `json:"title"`
}

// briefData is the whole project map grouped by kind.
type briefData struct {
	Features   []briefItem `json:"features"`
	Decisions  []briefItem `json:"decisions"`
	Runbooks   []briefItem `json:"runbooks"`
	Notes      []briefItem `json:"notes"`
	References []briefItem `json:"references"`
}

func newBriefCmd() *cobra.Command {
	var dir string
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "brief",
		Short: "Cheap project map: id + state + title of every entity (priming)",
		Long: "Prints a cheap project map — id, state and title of every entity (feature, decision, " +
			"runbook, note, reference), grouped by kind, WITHOUT paths or prose. Meant for loading the " +
			"whole map at once and then 'cerne show <id>' only on the ones that matter. By default shows the current " +
			"state; --all includes superseded/rejected (history). --json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrief(cmd, dir, all, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&all, "all", false, "include superseded/rejected entities (history)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runBrief(cmd *cobra.Command, dir string, all, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	data, err := collectBrief(ctx.repoDir, all)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	renderBrief(out, data)
	return nil
}

// collectBrief reads every entity from the backend store (not the .docs/
// projection, so it sees current state even when .docs/ is stale) and maps each
// to an id/state/title triple. Terminal entities (superseded/rejected/obsolete)
// are omitted unless all is set.
func collectBrief(repoDir string, all bool) (briefData, error) {
	var d briefData
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return d, err
	}
	for _, f := range features {
		if !all && isHistoryState(string(f.State)) {
			continue
		}
		d.Features = append(d.Features, briefItem{ID: f.ID, State: string(f.State), Title: f.Title})
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return d, err
	}
	for _, x := range decisions {
		if !all && isHistoryState(string(x.State)) {
			continue
		}
		d.Decisions = append(d.Decisions, briefItem{ID: x.ID, State: string(x.State), Title: x.Title})
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return d, err
	}
	for _, x := range runbooks {
		if !all && isHistoryState(string(x.State)) {
			continue
		}
		d.Runbooks = append(d.Runbooks, briefItem{ID: x.ID, State: string(x.State), Title: x.Title})
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return d, err
	}
	for _, x := range notes {
		if !all && isHistoryState(string(x.State)) {
			continue
		}
		d.Notes = append(d.Notes, briefItem{ID: x.ID, State: string(x.State), Title: x.Title})
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return d, err
	}
	for _, x := range references {
		if !all && isHistoryState(string(x.State)) {
			continue
		}
		d.References = append(d.References, briefItem{ID: x.ID, State: string(x.State), Title: x.Title})
	}
	return d, nil
}

// isHistoryState reports whether a state string is terminal (history): an entity
// in it is not part of the current map. Mirrors the ask retrieval filter.
func isHistoryState(s string) bool {
	switch s {
	case "superseded", "rejected", "obsolete", "retired":
		return true
	}
	return false
}

// renderBrief writes the cheap map: per non-empty kind, a "HEADER (n)" line then
// one "  <id>  <state>  <title>" row per entity, the state column padded for
// alignment. No paths, no bodies — that is the point.
func renderBrief(out io.Writer, d briefData) {
	groups := []struct {
		header string
		items  []briefItem
	}{
		{"FEATURES", d.Features},
		{"DECISIONS", d.Decisions},
		{"RUNBOOKS", d.Runbooks},
		{"NOTES", d.Notes},
		{"REFERENCES", d.References},
	}
	total := 0
	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		width := 0
		for _, it := range g.items {
			if n := len(it.State); n > width {
				width = n
			}
		}
		fmt.Fprintf(out, "%s (%d)\n", g.header, len(g.items))
		for _, it := range g.items {
			fmt.Fprintf(out, "  %s  %-*s  %s\n", it.ID, width, it.State, it.Title)
		}
		fmt.Fprintln(out)
		total += len(g.items)
	}
	if total == 0 {
		fmt.Fprintln(out, "Empty project. Create the first entity with 'cerne feature new'.")
		return
	}
	fmt.Fprintf(out, "Total: %d entity(ies)\n", total)
}
