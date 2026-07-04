package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// computeDriftFindings resolves the drift-suspect findings (F-0113) for a
// project: it diffs the user repo from the pinned project_head baseline to its
// current HEAD, then maps the changed paths to implemented features via the
// .cernerc module_paths globs. It is inert (nil findings) whenever there is no
// baseline, no commits, or no resolvable diff — the same opt-in stance the lint
// rule takes, so `cerne lint`/`cerne drift` never fail on a non-git or
// unconfigured project. It also returns the changed paths and baseline so the
// --draft path (F-0114) can ground its prompt without recomputing them.
func computeDriftFindings(ctx projCtx) (findings []lint.Finding, changed []string, baseline string, err error) {
	baseline, err = project.LoadProjectHead(ctx.projDir)
	if errors.Is(err, project.ErrNoProjectHead) {
		return nil, nil, "", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	head, err := gitback.HeadSHA(ctx.projDir)
	if errors.Is(err, gitback.ErrNoCommits) {
		return nil, nil, "", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	changed, err = gitback.ChangedPaths(ctx.projDir, baseline, head)
	if errors.Is(err, gitback.ErrNoBaseline) {
		return nil, nil, "", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return nil, nil, "", err
	}
	return lint.DriftFindings(changed, baseline, ctx.cfg.ModulePaths, features), changed, baseline, nil
}

func newDriftCmd() *cobra.Command {
	var dir string
	var asJSON bool
	var draft bool
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Sinaliza features implementadas cujo código mudou desde o último sync",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDrift(cmd, dir, asJSON, draft)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "saída em JSON")
	cmd.Flags().BoolVar(&draft, "draft", false, "emite um prompt de errata por finding em .docs/.drift-proposals/ (o agente redige; nada é aplicado)")
	return cmd
}

// ErrDraftJSON is returned when --draft is combined with --json: the draft path
// emits prompt bundles and a human contract, not machine output.
var ErrDraftJSON = errors.New("--draft e --json são incompatíveis")

// driftRow is the machine-readable form of a drift-suspect finding.
type driftRow struct {
	Rule   string `json:"rule"`
	Entity string `json:"entity"`
	Detail string `json:"detail"`
}

func runDrift(cmd *cobra.Command, dir string, asJSON, draft bool) error {
	if draft && asJSON {
		return ErrDraftJSON
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	findings, changed, baseline, err := computeDriftFindings(ctx)
	if err != nil {
		return err
	}
	lint.Sort(findings)
	out := cmd.OutOrStdout()
	if draft {
		return emitDriftDrafts(out, ctx, findings, changed, baseline)
	}
	if asJSON {
		rows := make([]driftRow, 0, len(findings))
		for _, f := range findings {
			rows = append(rows, driftRow{Rule: f.Rule, Entity: f.Entity, Detail: f.Detail})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(findings) == 0 {
		fmt.Fprintln(out, "Nenhum drift detectado.")
		return nil
	}
	printFindings(out, findings)
	return nil
}

// driftProposalsRelDir is the gitignored, per-worktree directory (under .docs/)
// holding the delegation prompt bundles --draft emits. Nothing here is ever
// committed or applied — the agent drafts the errata, the human applies it via
// `cerne feature errata` (D-0004).
const driftProposalsRelDir = ".docs/.drift-proposals"

// emitDriftDrafts (F-0114) writes one delegation prompt bundle per finding under
// .docs/.drift-proposals/<id>.prompt.md and prints the contract. It applies
// nothing: the host agent writes the errata draft into the sibling <id>.md, then
// the human runs `cerne feature errata`.
func emitDriftDrafts(out io.Writer, ctx projCtx, findings []lint.Finding, changed []string, baseline string) error {
	if len(findings) == 0 {
		fmt.Fprintln(out, "Nenhum drift detectado.")
		return nil
	}
	dir := filepath.Join(ctx.projDir, filepath.FromSlash(driftProposalsRelDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d rascunho(s) de errata para redigir (prompts em %s/):\n", len(findings), driftProposalsRelDir)
	for _, f := range findings {
		prompt, err := app.DriftDraftPrompt(ctx.repoDir, f.Entity, baseline, changed)
		if err != nil {
			return err
		}
		promptPath := filepath.Join(dir, f.Entity+".prompt.md")
		if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s → escreva %s/%s.md, depois: cerne feature errata %s --file %s/%s.md\n",
			f.Entity, driftProposalsRelDir, f.Entity, f.Entity, driftProposalsRelDir, f.Entity)
	}
	return nil
}
