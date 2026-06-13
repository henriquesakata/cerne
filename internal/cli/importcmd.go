package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/importer"
	"gitlab.com/rockt/cerne/internal/store"
)

// Dry-run plan breakdown orders; only the states the import heuristics can
// produce are listed.
var (
	planFeatureStates  = []string{"candidate", "in_development", "implemented", "superseded"}
	planDecisionStates = []string{"active", "superseded"}
)

func newImportCmd() *cobra.Command {
	var dir, root string
	var yes bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing documentation tree into the backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, dir, root, yes)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&root, "root", "", "documentation tree to scan (defaults to the project directory)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply the import instead of printing the dry-run plan")
	return cmd
}

func runImport(cmd *cobra.Command, dir, root string, yes bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if root == "" {
		root = ctx.projDir
	}
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("--root: %w", err)
	}
	proj, err := store.LoadProject(ctx.repoDir)
	if err != nil {
		return err
	}
	cands, err := importer.Scan(root, proj.Modules)
	if err != nil {
		return err
	}
	seeds, err := importer.LoadHistoricalSeed(root)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(cands) == 0 && len(seeds) == 0 {
		fmt.Fprintln(out, "Nada para importar.")
		return nil
	}
	if !yes {
		printImportPlan(out, cands, seeds)
		return nil
	}

	// Pre-validate the whole seed so a malformed entry aborts before any
	// write — the historicals apply atomically alongside the scanned docs.
	for _, s := range seeds {
		if err := domain.ValidateHistorical(s.Name, s.SupersededBy, s.Reason, s.Aliases); err != nil {
			return fmt.Errorf("historical.toml (%s): %w", s.Name, err)
		}
	}

	sum, err := importer.Apply(ctx.repoDir, cands, eventActor())
	if err != nil {
		return err
	}
	for _, s := range seeds {
		if _, _, err := app.SaveHistoricalEntity(ctx.repoDir, s.Name, s.SupersededBy, s.Reason, s.Aliases, "", "import"); err != nil {
			return err
		}
	}
	total := sum.Features + sum.Decisions + sum.References + len(seeds)
	sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: import %d documento(s)", total))
	if err != nil {
		return err
	}
	if err := updateLockSnapshot(ctx.projDir, sha); err != nil {
		return err
	}
	fmt.Fprintf(out, "Importado: %d features, %d decisões, %d referências, %d históricas (snapshot %s).\n",
		sum.Features, sum.Decisions, sum.References, len(seeds), shortSHA(sha))
	return nil
}

// printImportPlan writes the dry-run report: per-kind totals with a state
// breakdown, a sample of the candidates and the --yes hint.
func printImportPlan(w io.Writer, cands []importer.Candidate, seeds []importer.HistoricalSeed) {
	counts := map[string]map[string]int{} // kind -> state -> count
	for _, c := range cands {
		if counts[c.Kind] == nil {
			counts[c.Kind] = map[string]int{}
		}
		counts[c.Kind][c.State]++
	}
	fmt.Fprintln(w, "Plano de importação (dry-run):")
	fmt.Fprintf(w, "  Features: %d%s\n", kindTotal(counts["feature"]), stateBreakdown(counts["feature"], planFeatureStates))
	fmt.Fprintf(w, "  Decisões: %d%s\n", kindTotal(counts["decision"]), stateBreakdown(counts["decision"], planDecisionStates))
	fmt.Fprintf(w, "  Referências: %d\n", kindTotal(counts["reference"]))
	fmt.Fprintf(w, "  Históricas: %d\n", len(seeds))
	fmt.Fprintln(w, "Amostra:")
	for _, line := range sampleLines(cands) {
		fmt.Fprintln(w, line)
	}
	for _, s := range seeds {
		fmt.Fprintf(w, "  H? %s → historical superseded_by %s\n", s.Name, s.SupersededBy)
	}
	fmt.Fprintln(w, "Rode com --yes para aplicar.")
}

// kindTotal sums one kind's per-state counts.
func kindTotal(states map[string]int) int {
	total := 0
	for _, n := range states {
		total += n
	}
	return total
}

// stateBreakdown renders " (candidate 3, implemented 5)" listing the
// non-zero states in the given order; "" when there are none.
func stateBreakdown(states map[string]int, order []string) string {
	var parts []string
	for _, s := range order {
		if states[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s, states[s]))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// sampleLines renders the plan sample: every candidate when there are at
// most 6, otherwise the first 5, an ellipsis and the last one.
func sampleLines(cands []importer.Candidate) []string {
	line := func(c importer.Candidate) string {
		letter := strings.ToUpper(c.Kind[:1])
		return fmt.Sprintf("  %s? %s → %s %s", letter, c.Source, c.Kind, c.State)
	}
	if len(cands) <= 6 {
		lines := make([]string, 0, len(cands))
		for _, c := range cands {
			lines = append(lines, line(c))
		}
		return lines
	}
	lines := make([]string, 0, 7)
	for _, c := range cands[:5] {
		lines = append(lines, line(c))
	}
	lines = append(lines, "  ...")
	lines = append(lines, line(cands[len(cands)-1]))
	return lines
}
