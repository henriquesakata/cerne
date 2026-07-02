package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/lint"
)

func newLintCmd() *cobra.Command {
	var dir string
	var fixTasks bool
	var staged bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Detect stale documentation in the backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, dir, fixTasks, staged)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&fixTasks, "fix-tasks", false, "print a maintenance task list after the findings")
	cmd.Flags().BoolVar(&staged, "staged", false, "validar as edições locais em .docs/ antes do push, sem tocar o backend")
	return cmd
}

func runLint(cmd *cobra.Command, dir string, fixTasks, staged bool) error {
	repoDir, ignore, cleanup, err := lintRoot(dir, staged)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	findings, err := lint.Run(repoDir, ignore...)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(findings) == 0 {
		fmt.Fprintln(out, "Nenhum problema encontrado.")
		return nil
	}
	printFindings(out, findings)
	if fixTasks {
		printFixTasks(out, findings)
	}
	// Root has SilenceUsage, so this surfaces as a single error line and
	// exit code 1 via main.
	return fmt.Errorf("%d problema(s) encontrado(s)", len(findings))
}

// lintRoot resolves the directory lint.Run scans. Default: the cache clone,
// refreshed from origin (resolveProject). With staged: a temp backend-shaped
// tree of the local .docs/ edits over the pulled snapshot, WITHOUT a pull and
// without touching the backend (resolvePaths + stagedLintRoot). The returned
// cleanup is nil in the default mode and removes the temp tree in staged mode.
func lintRoot(dir string, staged bool) (repoDir string, ignore []string, cleanup func(), err error) {
	if staged {
		ctx, err := resolvePaths(dir)
		if err != nil {
			return "", nil, nil, err
		}
		root, cleanup, err := stagedLintRoot(ctx.repoDir, ctx.projDir)
		return root, ctx.cfg.UnregisteredSourceIgnore, cleanup, err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return "", nil, nil, err
	}
	return ctx.repoDir, ctx.cfg.UnregisteredSourceIgnore, nil, nil
}

// printFindings renders the findings grouped by rule, in lint.RuleOrder.
func printFindings(w io.Writer, findings []lint.Finding) {
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
		fmt.Fprintf(w, "%s (%d)\n", strings.ToUpper(rule), len(group))
		for _, f := range group {
			fmt.Fprintf(w, "  %s — %s\n", findingLoc(f), f.Detail)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Total: %d problema(s).\n", len(findings))
}

// findingLoc is the location shown for a finding: the file when known,
// the entity ID for entity-level findings (missing-gate-doc).
func findingLoc(f lint.Finding) string {
	if f.File != "" {
		return f.File
	}
	return f.Entity
}

// printFixTasks renders a markdown task list ready to paste into an AI
// agent, one imperative pt-BR task per finding.
func printFixTasks(w io.Writer, findings []lint.Finding) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Tarefas de manutenção (geradas por cerne lint)")
	for _, f := range findings {
		fmt.Fprintf(w, "- [ ] %s: %s\n", findingLoc(f), taskAction(f))
	}
}

// taskAction turns a finding into an imperative pt-BR maintenance task.
// The ref-to-superseded and missing-gate-doc branches parse the Detail
// formats produced by lint.Run; the lint package tests pin those formats.
func taskAction(f lint.Finding) string {
	switch f.Rule {
	case lint.RuleRefToSuperseded:
		// "menciona <ID> ("<Title>") que está <state>[ — use <X>]"
		rest := strings.TrimPrefix(f.Detail, "menciona ")
		mention, after, _ := strings.Cut(rest, " que está ")
		state, use, hasUse := strings.Cut(after, " — use ")
		if hasUse {
			return fmt.Sprintf("substituir menção a %s por %s", mention, use)
		}
		return fmt.Sprintf("remover menção a %s (%s)", mention, state)
	case lint.RuleBrokenLink:
		return "corrigir " + f.Detail
	case lint.RuleOrphanContent:
		return "remover conteúdo sem entidade ou criar a entidade correspondente"
	case lint.RuleMissingGateDoc:
		// "feature <state> sem <doc>.md real"
		if before, doc, found := strings.Cut(f.Detail, " sem "); found {
			return fmt.Sprintf("escrever %s (%s)", doc, before)
		}
	case lint.RuleEpicBehindChildren:
		return "escrever o plano do épico e iniciá-lo (cerne feature start)"
	case lint.RuleStaleCandidate:
		return "rejeitar a candidata (cerne feature reject) ou escrever a spec e aprovar"
	case lint.RuleStaleNote:
		return "aposentar (cerne note retire) ou marcar durável (cerne note keep)"
	}
	return f.Detail
}
