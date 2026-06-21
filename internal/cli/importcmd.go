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
	// Idempotency: skip documents already imported (matched by imported_from)
	// and historical seeds already declared (matched by name), so re-running
	// import brings in only what was added since the last run.
	imported, err := importer.ImportedSources(ctx.repoDir)
	if err != nil {
		return err
	}
	cands, skipped := importer.Unimported(cands, imported)
	seeds, seedSkipped, err := unseededHistoricals(ctx.repoDir, seeds)
	if err != nil {
		return err
	}
	skipped += seedSkipped

	out := cmd.OutOrStdout()
	nothingNew := len(cands) == 0 && len(seeds) == 0

	// Dry-run: show what would be imported (or that nothing is new), then what
	// the classify backfill would assign over the existing module-less features.
	if !yes {
		if nothingNew {
			reportNothingNew(out, skipped)
		} else {
			printImportPlan(out, cands, seeds, skipped)
		}
		assigned, residual, err := app.ClassifyBackfill(ctx.repoDir, "", false)
		if err != nil {
			return err
		}
		reportClassifyProjection(out, len(assigned), residual)
		return nil
	}

	// Apply the scanned docs, when there is anything new.
	if nothingNew {
		reportNothingNew(out, skipped)
	} else {
		// Pre-validate the whole seed so a malformed entry aborts before any
		// write — the historicals apply atomically alongside the scanned docs.
		for _, s := range seeds {
			if err := domain.ValidateHistorical(s.Name, s.SupersededBy, s.Reason, s.Aliases); err != nil {
				return fmt.Errorf("historical.toml (%s): %w", s.Name, err)
			}
		}

		// Permalink source-code references to the source repo's blob, pinned at
		// its current HEAD. Left disabled when root is not a git checkout or its
		// remote host is unrecognised, in which case code refs stay as written.
		opts := importer.ApplyOptions{SrcRoot: root}
		if url, err := gitback.RemoteURL(root); err == nil {
			if sha, err := gitback.HeadSHA(root); err == nil {
				opts.BlobBase = importer.BlobBase(url, sha)
			}
		}
		sum, err := importer.Apply(ctx.repoDir, cands, eventActor(), opts)
		if err != nil {
			return err
		}
		for _, s := range seeds {
			if _, _, err := app.SaveHistoricalEntity(ctx.repoDir, s.Name, s.SupersededBy, s.Reason, s.Aliases, "", "import"); err != nil {
				return err
			}
		}
		total := sum.Features + sum.Decisions + sum.References + sum.Runbooks + len(seeds)
		sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: import %d documento(s)", total))
		if err != nil {
			return err
		}
		if err := updateLockSnapshot(ctx.projDir, sha); err != nil {
			return err
		}
		fmt.Fprintf(out, "Importado: %d features, %d decisões, %d runbooks, %d referências, %d históricas (snapshot %s).\n",
			sum.Features, sum.Decisions, sum.Runbooks, sum.References, len(seeds), shortSHA(sha))
	}

	// Self-heal module assignment: classify every module-less feature the
	// heuristic resolves — so a module declared after its cases were imported
	// (e.g. a new subsystem) is picked up here instead of vanishing from the
	// wiki. ClassifyBackfill only ever touches features with an empty module and
	// commits at most once, so a no-op run writes nothing.
	assigned, residual, err := app.ClassifyBackfill(ctx.repoDir, ctx.projDir, true)
	if err != nil {
		return err
	}
	reportClassifyResult(out, len(assigned), residual)
	return nil
}

// reportNothingNew prints the import line when no scanned doc is new: either
// everything was already imported, or the tree had nothing to import.
func reportNothingNew(out io.Writer, skipped int) {
	if skipped > 0 {
		fmt.Fprintf(out, "Tudo já importado (%d documento(s) já no backend).\n", skipped)
	} else {
		fmt.Fprintln(out, "Nada para importar.")
	}
}

// reportClassifyProjection (dry-run) announces what the classify backfill would
// assign, only when it would assign at least one — silent otherwise to avoid
// noise (compile already reports the standing module-less count).
func reportClassifyProjection(out io.Writer, assigned, residual int) {
	if assigned > 0 {
		fmt.Fprintf(out, "Inferência classificaria %d feature(s) sem módulo (%d permaneceriam).\n", assigned, residual)
	}
}

// reportClassifyResult (apply) reports what the backfill classified, only when
// it assigned at least one.
func reportClassifyResult(out io.Writer, assigned, residual int) {
	if assigned > 0 {
		fmt.Fprintf(out, "Classificadas %d feature(s) por inferência (%d sem módulo permanecem).\n", assigned, residual)
	}
}

// unseededHistoricals drops seeds whose name already exists as a historical
// entity in the backend, so a re-run does not duplicate them; returns the kept
// seeds and the skipped count.
func unseededHistoricals(repoDir string, seeds []importer.HistoricalSeed) ([]importer.HistoricalSeed, int, error) {
	existing, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		return nil, 0, err
	}
	byName := make(map[string]bool, len(existing))
	for _, h := range existing {
		byName[h.Name] = true
	}
	var kept []importer.HistoricalSeed
	skipped := 0
	for _, s := range seeds {
		if byName[s.Name] {
			skipped++
			continue
		}
		kept = append(kept, s)
	}
	return kept, skipped, nil
}

// printImportPlan writes the dry-run report: per-kind totals with a state
// breakdown, a sample of the candidates and the --yes hint. skipped is the
// number of already-imported documents and seeds omitted from the plan.
func printImportPlan(w io.Writer, cands []importer.Candidate, seeds []importer.HistoricalSeed, skipped int) {
	counts := map[string]map[string]int{} // kind -> state -> count
	for _, c := range cands {
		if counts[c.Kind] == nil {
			counts[c.Kind] = map[string]int{}
		}
		counts[c.Kind][c.State]++
	}
	fmt.Fprintln(w, "Plano de importação (dry-run):")
	if skipped > 0 {
		fmt.Fprintf(w, "  Já importados (pulados): %d\n", skipped)
	}
	fmt.Fprintf(w, "  Features: %d%s\n", kindTotal(counts["feature"]), stateBreakdown(counts["feature"], planFeatureStates))
	fmt.Fprintf(w, "  Decisões: %d%s\n", kindTotal(counts["decision"]), stateBreakdown(counts["decision"], planDecisionStates))
	fmt.Fprintf(w, "  Runbooks: %d\n", kindTotal(counts["runbook"]))
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
