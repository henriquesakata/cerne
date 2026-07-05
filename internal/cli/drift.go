package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/llm"
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
	return lint.DriftFindings(changed, baseline, ctx.cfg.ModulePaths, ctx.cfg.ComponentPaths, features), changed, baseline, nil
}

func newDriftCmd() *cobra.Command {
	var dir string
	var asJSON bool
	var draft bool
	var auto bool
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Sinaliza features implementadas cujo código mudou desde o último sync",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDrift(cmd, dir, asJSON, draft, auto)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "saída em JSON")
	cmd.Flags().BoolVar(&draft, "draft", false, "emite um prompt de errata por finding em .docs/.drift-proposals/ (o agente redige; nada é aplicado)")
	cmd.Flags().BoolVar(&auto, "auto", false, "redige via LLM e aplica a errata automaticamente, registrando pendência de revisão (D-0005)")
	cmd.AddCommand(newDriftPendingCmd())
	cmd.AddCommand(newDriftAckCmd())
	return cmd
}

// ErrDraftJSON is returned when --draft is combined with --json: the draft path
// emits prompt bundles and a human contract, not machine output.
var ErrDraftJSON = errors.New("--draft e --json são incompatíveis")

// ErrAutoExclusive is returned when --auto is combined with --draft or --json:
// the auto path applies erratas and reports a human summary.
var ErrAutoExclusive = errors.New("--auto é incompatível com --draft e --json")

// driftRow is the machine-readable form of a drift-suspect finding.
type driftRow struct {
	Rule   string `json:"rule"`
	Entity string `json:"entity"`
	Detail string `json:"detail"`
}

func runDrift(cmd *cobra.Command, dir string, asJSON, draft, auto bool) error {
	if draft && asJSON {
		return ErrDraftJSON
	}
	if auto && (draft || asJSON) {
		return ErrAutoExclusive
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
	if auto {
		return runDriftAuto(cmd, ctx, findings, changed, baseline)
	}
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
// `cerne feature errata` (the gated path D-0005 keeps alongside --auto).
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

// runDriftAuto (F-0115, D-0005 apply-then-notify) closes the loop for every
// finding: draft via the configured LLM provider, apply as an errata under the
// drift-auto actor, record the pending-review notification. Per-finding
// failures are reported and skipped; the command exits non-zero if any failed
// (already-applied erratas stay — they are valid annotations).
func runDriftAuto(cmd *cobra.Command, ctx projCtx, findings []lint.Finding, changed []string, baseline string) error {
	out := cmd.OutOrStdout()
	if len(findings) == 0 {
		fmt.Fprintln(out, "Nenhum drift detectado.")
		return nil
	}
	provider, err := llm.NewProvider(ctx.cfg.LLM)
	if errors.Is(err, llm.ErrNoProviderForDelegation) {
		return fmt.Errorf("modo [llm] delegation não tem provider chamável — configure mode=\"claude-cli\" ou \"byo-key\" no .cernerc, ou use 'cerne drift --draft' (o agente host redige)")
	}
	if err != nil {
		return err
	}
	var applied, failed int
	for _, f := range findings {
		if _, err := app.AutoDriftErrata(cmd.Context(), ctx.repoDir, ctx.projDir, f.Entity, baseline, changed, provider); err != nil {
			failed++
			fmt.Fprintf(cmd.ErrOrStderr(), "✗ %s: %v\n", f.Entity, err)
			continue
		}
		applied++
		fmt.Fprintf(out, "✓ %s: errata automática aplicada\n", f.Entity)
	}
	if applied > 0 {
		fmt.Fprintf(out, "%d errata(s) aplicada(s) — revise com 'cerne drift pending' e reconheça com 'cerne drift ack <id>'.\n", applied)
	}
	if failed > 0 {
		return fmt.Errorf("%d finding(s) falharam", failed)
	}
	return nil
}

// pendingRow is the machine-readable form of one pending automatic update.
type pendingRow struct {
	Entity   string    `json:"entity"`
	At       time.Time `json:"at"`
	Baseline string    `json:"baseline"`
	Paths    string    `json:"paths"`
}

func newDriftPendingCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Lista atualizações automáticas de drift aguardando revisão humana",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(dir)
			if err != nil {
				return err
			}
			pending, err := app.PendingDriftReviews(ctx.repoDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]pendingRow, 0, len(pending))
				for _, p := range pending {
					rows = append(rows, pendingRow{Entity: p.Entity, At: p.At, Baseline: p.Baseline, Paths: p.Paths})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			if len(pending) == 0 {
				fmt.Fprintln(out, "Nenhuma atualização automática pendente de revisão.")
				return nil
			}
			fmt.Fprintf(out, "%d atualização(ões) automática(s) aguardando revisão:\n", len(pending))
			for _, p := range pending {
				fmt.Fprintf(out, "  %s  %s  (desde %s: %s)\n", p.Entity, p.At.Format("2006-01-02 15:04"), p.Baseline, p.Paths)
			}
			fmt.Fprintln(out, "Revise com 'cerne show <id>' e reconheça com 'cerne drift ack <id>' (ou corrija com nova errata).")
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "saída em JSON")
	return cmd
}

func newDriftAckCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "ack <id>",
		Short: "Reconhece as atualizações automáticas de drift de uma entidade",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(dir)
			if err != nil {
				return err
			}
			err = app.AckDrift(ctx.repoDir, ctx.projDir, args[0], eventActor())
			if errors.Is(err, app.ErrNoPendingDrift) {
				fmt.Fprintf(cmd.OutOrStdout(), "%s não tem atualização automática pendente.\n", args[0])
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ %s reconhecida — fora da lista de pendências.\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}
