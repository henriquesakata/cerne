package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/gauntlet"
	"github.com/henriquesakata/cerne/internal/store"
)

func newGauntletCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gauntlet",
		Short: "Tools for the Gauntlet protocol",
	}
	cmd.AddCommand(newGauntletArtifactCmd())
	return cmd
}

func newGauntletArtifactCmd() *cobra.Command {
	var dir, base, notes, out string
	var excludes []string
	cmd := &cobra.Command{
		Use:   "artifact <id>",
		Short: "Build the critic's bundle: spec criteria, plan contracts and the diff without noise",
		Long: "Writes one file with two fenced parts: the spec's acceptance criteria plus the plan's contracts " +
			"(and, with --notes, the builder's declared deviations), then the diff of the current branch against the base. " +
			"The base defaults to the merge-base with origin/<pin_branch>, origin/HEAD or main, in that order. " +
			"ui/dist and docs.lock are left out of the diff; --exclude adds paths. Read-only: nothing is written to the backend.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGauntletArtifact(cmd, dir, args[0], base, notes, out, excludes)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "diff base ref (default: origin/<pin_branch>, origin/HEAD or main)")
	cmd.Flags().StringArrayVar(&excludes, "exclude", nil, "path glob to leave out of the diff (repeatable; ui/dist and docs.lock always are)")
	cmd.Flags().StringVar(&notes, "notes", "", "file with the builder's declared deviations")
	cmd.Flags().StringVar(&out, "out", "", "write the bundle to this path instead of stdout")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runGauntletArtifact(cmd *cobra.Command, dir, id, base, notesPath, outPath string, excludes []string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if _, err := store.LoadFeature(ctx.repoDir, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s not found in the backend", id)
		}
		return err
	}
	var notes string
	if notesPath != "" {
		b, err := os.ReadFile(notesPath)
		if err != nil {
			return fmt.Errorf("reading --notes: %w", err)
		}
		notes = string(b)
	}
	diff, usedBase, err := gauntlet.Diff(ctx.projDir, base, ctx.cfg.PinBranch, excludes)
	if err != nil {
		return err
	}
	text := gauntlet.Assemble(gauntlet.Input{
		Lang:  store.DocsLanguage(ctx.repoDir),
		Spec:  readFeatureDoc(ctx.repoDir, id, "spec.md"),
		Plan:  readFeatureDoc(ctx.repoDir, id, "plan.md"),
		Notes: notes,
		Diff:  diff,
	})
	if outPath == "" {
		fmt.Fprint(cmd.OutOrStdout(), text)
		return nil
	}
	if err := os.WriteFile(outPath, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "cerne: gauntlet artifact for %s → %s (%d bytes; diff %s..HEAD, excluded: %s)\n",
		id, outPath, len(text), usedBase, strings.Join(append([]string{"ui/dist", "docs.lock"}, excludes...), ", "))
	return nil
}
