package cli

import (
	"encoding/json"
	"fmt"
	"github.com/henriquesakata/cerne/internal/app"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/harness"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

func newLintCmd() *cobra.Command {
	var dir string
	var fixTasks bool
	var staged, all bool
	var feature, parent string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Detect stale documentation in the backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Changed, not != "": a blank --feature must fail, not widen to the project.
			for _, name := range []string{"feature", "parent"} {
				if v, _ := cmd.Flags().GetString(name); cmd.Flags().Changed(name) && strings.TrimSpace(v) == "" {
					return fmt.Errorf("--%s needs a feature id", name)
				}
			}
			if all && !staged {
				return fmt.Errorf("--all needs --staged")
			}
			scoped := cmd.Flags().Changed("feature") || cmd.Flags().Changed("parent")
			return runLint(cmd, dir, fixTasks, staged, all, scoped, feature, parent)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&fixTasks, "fix-tasks", false, "print a maintenance task list after the findings")
	cmd.Flags().BoolVar(&staged, "staged", false, "validate local .docs/ edits before push, without touching the backend")
	cmd.Flags().BoolVar(&all, "all", false, "with --staged: list and fail on every finding, not only those from your edits")
	cmd.Flags().StringVar(&feature, "feature", "", "only the findings about this feature")
	cmd.Flags().StringVar(&parent, "parent", "", "only the findings about this epic and its descendants")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the findings as a JSON array")
	cmd.MarkFlagsMutuallyExclusive("feature", "parent")
	return cmd
}

func runLint(cmd *cobra.Command, dir string, fixTasks, staged, all, scoped bool, feature, parent string) error {
	repoDir, cfg, cleanup, driftCtx, edits, err := lintRoot(dir, staged)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	findings, err := app.ProjectLint(repoDir, cfg)
	if err != nil {
		return err
	}
	// Drift-suspect (F-0113) needs the user's project repo, which lint.Run never
	// sees; it only runs in the default mode (driftCtx != nil), never in --staged
	// where there is no code diff to consider.
	if driftCtx != nil {
		drift, _, _, err := computeDriftFindings(*driftCtx)
		if err != nil {
			return err
		}
		findings = append(findings, drift...)
		// Hook citations (F-0340) read the project's hooks, so they share the
		// drift branch: default mode only.
		hooks, err := harness.ProjectHookTexts(driftCtx.projDir)
		if err != nil {
			return err
		}
		cites, err := lint.CheckHookCitations(repoDir, hooks)
		if err != nil {
			return err
		}
		findings = append(findings, cites...)
		lint.Sort(findings)
	}
	// Spec CLI literals in pt-BR (F-0353): only a project that declares its CLI
	// output English asks for them.
	if project.CLIOutputLanguage(dir) == "en" {
		lits, err := lint.SpecCLILiteralFindings(repoDir)
		if err != nil {
			return err
		}
		findings = append(findings, lits...)
		lint.Sort(findings)
	}
	lint.DowngradeWikiDebt(findings, wikiEnabled(cmd, dir))
	out := cmd.OutOrStdout()
	var label string
	var outside int
	if scoped {
		ids, l, err := lintScope(repoDir, feature, parent)
		if err != nil {
			return err
		}
		label = l
		findings, outside = lint.Scope(findings, ids)
	}
	// Staged without --all (and without an explicit scope): only the edit's
	// findings fail; the rest is a count (F-0382).
	editOnly := staged && !all && !scoped
	var otherCount int
	if editOnly {
		if len(edits.files) == 0 {
			if asJSON, _ := cmd.Flags().GetBool("json"); !asJSON {
				fmt.Fprintln(out, "cerne: no local edits to lint")
				return nil
			}
		}
		var rest []lint.Finding
		findings, rest = classifyStaged(findings, edits)
		otherCount = len(rest)
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		rows := make([]nextFinding, 0, len(findings))
		for _, f := range findings {
			rows = append(rows, newNextFinding(f))
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		var payload any = rows
		if editOnly {
			payload = struct {
				Findings   []nextFinding `json:"findings"`
				OtherCount int           `json:"other_count"`
			}{rows, otherCount}
		}
		if err := enc.Encode(payload); err != nil {
			return err
		}
		return lintFailure(findings)
	}
	if editOnly {
		defer func() {
			if otherCount > 0 {
				fmt.Fprintf(out, "cerne: %d other findings outside your edits (run cerne lint for the list)\n", otherCount)
			}
		}()
	}
	if len(findings) == 0 {
		if !scoped {
			fmt.Fprintln(out, "No problems found.")
		} else if outside == 0 {
			fmt.Fprintf(out, "No problems found in %s.\n", label)
		} else {
			fmt.Fprintf(out, "No problems found in %s (%d problem(s) outside the scope — run 'cerne lint' for the full list).\n", label, outside)
		}
		return nil
	}
	if scoped {
		printFindingsTotal(out, findings, fmt.Sprintf(" in %s (%d outside the scope)", label, outside))
	} else {
		printFindings(out, findings)
	}
	if fixTasks {
		printFixTasks(out, findings)
	}
	return lintFailure(findings)
}

// lintFailure is the exit policy of a lint gate: an error counting the
// blocking findings, nil when every finding is advisory (F-0322) or wiki debt
// downgraded by a disabled wiki (F-0316). Root has
// SilenceUsage, so the error surfaces as a single line and exit code 1.
func lintFailure(findings []lint.Finding) error {
	blocking := 0
	for _, f := range findings {
		if lint.Blocking(f) {
			blocking++
		}
	}
	if blocking == 0 {
		return nil
	}
	return fmt.Errorf("%d problem(s) found", blocking)
}

// lintScope resolves --feature / --parent to the set of entity ids whose
// findings are kept, plus the label shown in the output. --parent covers the
// epic and every descendant; the visited set guards against a parent cycle.
func lintScope(repoDir, feature, parent string) (ids map[string]bool, label string, err error) {
	id := domain.CanonicalID(feature + parent)
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, "", err
	}
	found := false
	for _, f := range features {
		if f.ID == id {
			found = true
			break
		}
	}
	if !found {
		return nil, "", fmt.Errorf("feature %s not found in backend", id)
	}
	ids = map[string]bool{id: true}
	if parent == "" {
		return ids, id, nil
	}
	children := store.ChildrenByParent(features) // parents load canonical (F-0326)
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if !ids[c.ID] {
				ids[c.ID] = true
				queue = append(queue, c.ID)
			}
		}
	}
	return ids, fmt.Sprintf("%s and %d descendant(s)", id, len(ids)-1), nil
}

// lintRoot resolves the directory lint.Run scans. Default: the cache clone,
// refreshed from origin (resolveProject). With staged: a temp backend-shaped
// tree of the local .docs/ edits over the pulled snapshot, WITHOUT a pull and
// without touching the backend (resolvePaths + stagedLintRoot). The returned
// cleanup is nil in the default mode and removes the temp tree in staged mode.
// The returned driftCtx is non-nil only in the default mode: it carries the
// resolved project so runLint can compute drift-suspect findings (F-0113). In
// staged mode it is nil — staged validates local .docs/ edits, not a code diff.
func lintRoot(dir string, staged bool) (repoDir string, cfg project.Config, cleanup func(), driftCtx *projCtx, edits stagedEdits, err error) {
	if staged {
		ctx, err := resolvePaths(dir)
		if err != nil {
			return "", project.Config{}, nil, nil, stagedEdits{}, err
		}
		root, edits, cleanup, err := stagedLintRoot(ctx.repoDir, ctx.projDir)
		return root, ctx.cfg, cleanup, nil, edits, err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return "", project.Config{}, nil, nil, stagedEdits{}, err
	}
	return ctx.repoDir, ctx.cfg, nil, &ctx, stagedEdits{}, nil
}

// printFindings renders the findings grouped by rule, in lint.RuleOrder.
func printFindings(w io.Writer, findings []lint.Finding) {
	printFindingsTotal(w, findings, "")
}

// printFindingsTotal is printFindings with a suffix on the Total line (the
// scope label of `lint --feature/--parent`).
func printFindingsTotal(w io.Writer, findings []lint.Finding, totalSuffix string) {
	for _, rule := range lint.RuleOrder {
		var group []lint.Finding
		for _, f := range findings {
			if f.Rule == rule {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			continue
		}
		warning := ""
		if lint.Advisory(rule) {
			warning = " (warning)"
		} else if group[0].Downgraded {
			warning = " (warning: wiki.enabled is off)"
		}
		fmt.Fprintf(w, "%s (%d)%s\n", strings.ToUpper(rule), len(group), warning)
		for _, f := range group {
			fmt.Fprintf(w, "  %s — %s\n", findingPos(f), f.Detail)
		}
		fmt.Fprintln(w)
	}
	advisory := 0
	for _, f := range findings {
		if !lint.Blocking(f) {
			advisory++
		}
	}
	switch {
	case advisory == len(findings):
		totalSuffix += ", all advisory — lint passes"
	case advisory > 0:
		totalSuffix += fmt.Sprintf(", %d advisory", advisory)
	}
	fmt.Fprintf(w, "Total: %d problem(s)%s.\n", len(findings), totalSuffix)
}

// findingLoc is the location shown for a finding: the file when known,
// the entity ID for entity-level findings (missing-gate-doc).
func findingLoc(f lint.Finding) string {
	if f.File != "" {
		return f.File
	}
	return f.Entity
}

// findingPos is findingLoc as `file:line` when the finding has a line (F-0352).
func findingPos(f lint.Finding) string {
	if f.File != "" && f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return findingLoc(f)
}

// printFixTasks renders a markdown task list ready to paste into an AI
// agent, one imperative task per finding.
func printFixTasks(w io.Writer, findings []lint.Finding) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Maintenance tasks (generated by cerne lint)")
	for _, f := range findings {
		// An advisory finding is reported, never queued as work (F-0316).
		if lint.Blocking(f) {
			fmt.Fprintf(w, "- [ ] %s: %s\n", findingPos(f), taskAction(f))
		}
	}
}

// taskAction turns a finding into an imperative maintenance task.
// The ref-to-superseded and missing-gate-doc branches parse the Detail
// formats produced by lint.Run; the lint package tests pin those formats. A
// finding that carries its Fix (an append-only errata, F-0329) is never
// parsed: its file cannot be edited, so the task is that command.
func taskAction(f lint.Finding) string {
	if f.Fix != "" {
		return "append an errata entry (the errata is append-only): " + f.Fix
	}
	switch f.Rule {
	case lint.RuleRefToSuperseded:
		// "mentions <ID> ("<Title>"), which is <state>[ — use <X>]"
		rest := strings.TrimPrefix(f.Detail, "mentions ")
		mention, after, _ := strings.Cut(rest, ", which is ")
		state, use, hasUse := strings.Cut(after, " — use ")
		if hasUse {
			return fmt.Sprintf("replace the mention of %s with %s", mention, use)
		}
		return fmt.Sprintf("remove the mention of %s (%s)", mention, state)
	case lint.RuleBrokenLink:
		return "fix " + f.Detail
	case lint.RuleOrphanContent:
		return "remove the entity-less content or create the matching entity"
	case lint.RuleMissingGateDoc:
		// "<state> feature without a real <doc>.md"
		if state, doc, found := strings.Cut(f.Detail, " feature without a real "); found {
			return fmt.Sprintf("write %s (%s feature)", doc, state)
		}
	case lint.RuleEpicBehindChildren:
		return "write the epic's plan and start it (cerne feature start)"
	case lint.RuleStaleCandidate:
		return "reject the candidate (cerne feature reject) or write the spec and approve it"
	case lint.RuleStaleReady:
		return "validate and accept it (cerne feature accept) or investigate why it stalled"
	case lint.RuleStaleWork:
		return "finish it (cerne work done) or abandon it (cerne work abandon)"
	case lint.RuleStaleNote:
		return "retire it (cerne note retire) or mark it durable (cerne note keep)"
	case lint.RuleStalePending:
		return "resolve, drop or promote it (cerne pending resolve|drop|promote)"
	}
	return f.Detail
}

// wikiEnabled is project.WikiEnabled for a command that gates on it (F-0316):
// a broken wiki.enabled source falls through as resolved, with its warning on
// stderr so the wiki never flips back on in silence.
func wikiEnabled(cmd *cobra.Command, dir string) bool {
	on, warning := project.WikiEnabled(dir)
	if warning != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), warning)
	}
	return on
}
