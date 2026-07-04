package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// computeDriftFindings resolves the drift-suspect findings (F-0113) for a
// project: it diffs the user repo from the pinned project_head baseline to its
// current HEAD, then maps the changed paths to implemented features via the
// .cernerc module_paths globs. It is inert (nil, nil) whenever there is no
// baseline, no commits, or no resolvable diff — the same opt-in stance the lint
// rule takes, so `cerne lint`/`cerne drift` never fail on a non-git or
// unconfigured project.
func computeDriftFindings(ctx projCtx) ([]lint.Finding, error) {
	baseline, err := project.LoadProjectHead(ctx.projDir)
	if errors.Is(err, project.ErrNoProjectHead) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	head, err := gitback.HeadSHA(ctx.projDir)
	if errors.Is(err, gitback.ErrNoCommits) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	changed, err := gitback.ChangedPaths(ctx.projDir, baseline, head)
	if errors.Is(err, gitback.ErrNoBaseline) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return nil, err
	}
	return lint.DriftFindings(changed, baseline, ctx.cfg.ModulePaths, features), nil
}

func newDriftCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Sinaliza features implementadas cujo código mudou desde o último sync",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDrift(cmd, dir, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "saída em JSON")
	return cmd
}

// driftRow is the machine-readable form of a drift-suspect finding.
type driftRow struct {
	Rule   string `json:"rule"`
	Entity string `json:"entity"`
	Detail string `json:"detail"`
}

func runDrift(cmd *cobra.Command, dir string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	findings, err := computeDriftFindings(ctx)
	if err != nil {
		return err
	}
	lint.Sort(findings)
	out := cmd.OutOrStdout()
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
