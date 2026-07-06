package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/importer"
	"gitlab.com/rockt/cerne/internal/llm"
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
	var yes, agent, ingest bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing documentation tree into the backend",
		Long: "Imports an existing documentation tree. Without flags, it uses the " +
			"Rockt Hub convention (deterministic). With --agent, it does an AI-assisted generic import of " +
			"any structure: it emits a mapping prompt (delegation mode) or generates the plan " +
			"(byo-key); then 'cerne import --agent --ingest' validates and applies it (--yes).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, dir, root, yes, agent, ingest)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&root, "root", "", "documentation tree to scan (defaults to the project directory)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply the import instead of printing the dry-run plan")
	cmd.Flags().BoolVar(&agent, "agent", false, "AI-assisted generic import: emit a mapping prompt for any doc structure")
	cmd.Flags().BoolVar(&ingest, "ingest", false, "ingest the agent's plan.json: validate, then dry-run (or --yes to apply)")
	return cmd
}

func runImport(cmd *cobra.Command, dir, root string, yes, agent, ingest bool) error {
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
	// Agent path (F-0061) is fully gated behind --agent: without it, the
	// deterministic hub import below runs exactly as before.
	if agent {
		if ingest {
			return runImportAgentIngest(cmd, ctx, yes)
		}
		return runImportAgent(cmd, ctx, root)
	}
	if ingest {
		return fmt.Errorf("--ingest requires --agent")
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
	// Second guard: drop feature candidates whose title already exists as a
	// backend feature. Catches cases registered natively (cerne feature new) —
	// they carry no imported_from, so the source-based dedup above lets them
	// through — preventing import from minting a duplicate. A genuine orphan
	// (no matching backend title) is still surfaced.
	registered, err := importer.ExistingFeatureTitles(ctx.repoDir)
	if err != nil {
		return err
	}
	cands, registeredSkipped := importer.DropRegisteredTitles(cands, registered)
	skipped += registeredSkipped
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
		sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: import %d document(s)", total))
		if err != nil {
			return err
		}
		if err := updateLockSnapshot(ctx.projDir, sha); err != nil {
			return err
		}
		fmt.Fprintf(out, "Imported: %d features, %d decisions, %d runbooks, %d references, %d historicals (snapshot %s).\n",
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
		fmt.Fprintf(out, "Everything already imported (%d document(s) already in the backend).\n", skipped)
	} else {
		fmt.Fprintln(out, "Nothing to import.")
	}
}

// reportClassifyProjection (dry-run) announces what the classify backfill would
// assign, only when it would assign at least one — silent otherwise to avoid
// noise (compile already reports the standing module-less count).
func reportClassifyProjection(out io.Writer, assigned, residual int) {
	if assigned > 0 {
		fmt.Fprintf(out, "Inference would classify %d feature(s) without a module (%d would remain).\n", assigned, residual)
	}
}

// reportClassifyResult (apply) reports what the backfill classified, only when
// it assigned at least one.
func reportClassifyResult(out io.Writer, assigned, residual int) {
	if assigned > 0 {
		fmt.Fprintf(out, "Classified %d feature(s) by inference (%d without a module remain).\n", assigned, residual)
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
	fmt.Fprintln(w, "Import plan (dry-run):")
	if skipped > 0 {
		fmt.Fprintf(w, "  Already imported (skipped): %d\n", skipped)
	}
	fmt.Fprintf(w, "  Features: %d%s\n", kindTotal(counts["feature"]), stateBreakdown(counts["feature"], planFeatureStates))
	fmt.Fprintf(w, "  Decisions: %d%s\n", kindTotal(counts["decision"]), stateBreakdown(counts["decision"], planDecisionStates))
	fmt.Fprintf(w, "  Runbooks: %d\n", kindTotal(counts["runbook"]))
	fmt.Fprintf(w, "  References: %d\n", kindTotal(counts["reference"]))
	fmt.Fprintf(w, "  Historicals: %d\n", len(seeds))
	fmt.Fprintln(w, "Sample:")
	for _, line := range sampleLines(cands) {
		fmt.Fprintln(w, line)
	}
	for _, s := range seeds {
		fmt.Fprintf(w, "  H? %s → historical superseded_by %s\n", s.Name, s.SupersededBy)
	}
	fmt.Fprintln(w, "Run with --yes to apply.")
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

// agent-import handoff files under the project's .docs/, mirroring compile's
// .docs/wiki/.pending: phase 1 writes the prompt (delegation) or the plan
// (byo-key) here; phase 2 reads the plan from here.
const (
	agentPromptRel = ".docs/import/.pending/plan.prompt.md"
	agentPlanRel   = ".docs/import/.pending/plan.json"
)

// runImportAgent is phase 1 of agent-assisted import: collect the doc sources
// generically and produce the mapping prompt. In delegation mode (default) it
// writes the prompt for the host agent; in byo-key/claude-cli it synthesizes
// the plan in one Provider call. Either way it stops before applying, so the
// plan can be reviewed and ingested with --ingest.
func runImportAgent(cmd *cobra.Command, ctx projCtx, root string) error {
	out := cmd.OutOrStdout()
	sources, err := importer.CollectSources(root)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		fmt.Fprintln(out, "No documentation source found to map.")
		return nil
	}
	prompt := importer.BuildPlanPrompt(sources)
	if err := os.MkdirAll(filepath.Join(ctx.projDir, filepath.FromSlash(".docs/import/.pending")), 0o755); err != nil {
		return err
	}
	planPath := filepath.Join(ctx.projDir, filepath.FromSlash(agentPlanRel))

	// byo-key/claude-cli: synthesize the plan in one shot (same routing as compile).
	if llm.ResolveMode(ctx.cfg.LLM.Mode) != llm.ModeDelegation {
		prov, err := llm.NewProvider(ctx.cfg.LLM)
		if err != nil {
			return err
		}
		planJSON, err := prov.Complete(cmd.Context(), prompt)
		if err != nil {
			return fmt.Errorf("generating the plan: %w", err)
		}
		if err := os.WriteFile(planPath, []byte(planJSON), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "%d source(s) mapped. Plan at %s.\n", len(sources), agentPlanRel)
		fmt.Fprintln(out, "Review it and run 'cerne import --agent --ingest' (--yes to apply).")
		return nil
	}

	// delegation (default): emit the prompt for the host agent to fill.
	promptPath := filepath.Join(ctx.projDir, filepath.FromSlash(agentPromptRel))
	if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d source(s) collected. Mapping prompt at %s.\n", len(sources), agentPromptRel)
	fmt.Fprintf(out, "Process the prompt, write the JSON plan to %s and run 'cerne import --agent --ingest' (--yes to apply).\n", agentPlanRel)
	return nil
}

// runImportAgentIngest is phase 2: read the agent's plan.json, validate it
// against the cerne model (cerne is the authority), drop already-imported items
// (idempotency), then either print the dry-run plan or apply it (--yes).
func runImportAgentIngest(cmd *cobra.Command, ctx projCtx, yes bool) error {
	out := cmd.OutOrStdout()
	planPath := filepath.Join(ctx.projDir, filepath.FromSlash(agentPlanRel))
	data, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("reading the plan at %s: %w — run 'cerne import --agent' first", agentPlanRel, err)
	}
	plan, err := importer.ParsePlan(data)
	if err != nil {
		return err
	}
	if err := importer.ValidatePlan(plan); err != nil {
		return err
	}
	kept, skipped, err := importer.FilterPlanItems(ctx.repoDir, plan.Items)
	if err != nil {
		return err
	}

	if len(kept) == 0 {
		if skipped > 0 {
			fmt.Fprintf(out, "Everything already imported (%d item(s) already in the backend).\n", skipped)
		} else {
			fmt.Fprintln(out, "Nothing to import.")
		}
		return nil
	}

	// Dry-run: show the plan, write nothing.
	if !yes {
		printAgentPlan(out, kept, skipped)
		return nil
	}

	sum, err := importer.ApplyPlan(ctx.repoDir, kept, eventActor())
	if err != nil {
		return err
	}
	total := sum.Features + sum.Decisions + sum.References + sum.Runbooks + sum.Notes
	sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: import (agent) %d document(s)", total))
	if err != nil {
		return err
	}
	if err := updateLockSnapshot(ctx.projDir, sha); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(ctx.projDir, filepath.FromSlash(".docs/import/.pending")))
	fmt.Fprintf(out, "Imported (agent): %d features, %d decisions, %d references, %d runbooks, %d notes (snapshot %s).\n",
		sum.Features, sum.Decisions, sum.References, sum.Runbooks, sum.Notes, shortSHA(sha))
	return nil
}

// printAgentPlan writes the agent-import dry-run report: per-kind totals
// (notes included, unlike the hub printer), a small sample, and the --yes hint.
func printAgentPlan(w io.Writer, items []importer.PlanItem, skipped int) {
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Kind]++
	}
	fmt.Fprintln(w, "Import plan (agent, dry-run):")
	if skipped > 0 {
		fmt.Fprintf(w, "  Already imported (skipped): %d\n", skipped)
	}
	fmt.Fprintf(w, "  Features: %d\n", counts["feature"])
	fmt.Fprintf(w, "  Decisions: %d\n", counts["decision"])
	fmt.Fprintf(w, "  References: %d\n", counts["reference"])
	fmt.Fprintf(w, "  Runbooks: %d\n", counts["runbook"])
	fmt.Fprintf(w, "  Notes: %d\n", counts["note"])
	fmt.Fprintln(w, "Sample:")
	for i, it := range items {
		if i >= 8 {
			fmt.Fprintf(w, "  … (+%d)\n", len(items)-8)
			break
		}
		fmt.Fprintf(w, "  %s? %s → %s %s\n", strings.ToUpper(it.Kind[:1]), it.Source, it.Kind, it.State)
	}
	fmt.Fprintln(w, "Run with --yes to apply.")
}
