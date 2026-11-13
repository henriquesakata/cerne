package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	cmd.AddCommand(newGauntletRecordCmd())
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
			"ui/dist and docs.lock are left out of the diff; --exclude adds paths. Read-only: nothing is written to the backend. " +
			"The spec and plan are read from the backend: push .docs/ edits first.",
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
			return fmt.Errorf("feature %s not found in the backend: %w", id, err)
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
	text, err := gauntlet.Assemble(gauntlet.Input{
		Lang:  store.DocsLanguage(ctx.repoDir),
		Spec:  readFeatureDoc(ctx.repoDir, id, "spec.md"),
		Plan:  readFeatureDoc(ctx.repoDir, id, "plan.md"),
		Notes: notes,
		Diff:  diff,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
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

var (
	errGauntletRecordMode      = errors.New("pass either --verdict <file> --ours A|B, or --critic \"<line>\" (not both)")
	errGauntletNotMaterialized = errors.New("the feature's result is not in .docs/ (only active features are materialized)")
)

func newGauntletRecordCmd() *cobra.Command {
	var dir, verdict, ours, quote, critic string
	cmd := &cobra.Command{
		Use:   "record <id>",
		Short: "Record a Gauntlet round, or the critic's findings, in the result",
		Long: "Writes one line into the '## Gauntlet' of the feature's result in .docs/ (run 'cerne push' to send it). " +
			"--verdict <file> --ours A|B reads the judge's reply: its last non-empty line must be VEREDITO: A|B|EMPATE " +
			"(or VERDICT: A|B|TIE); the line recorded is the next round, as win, loss or tie for our side. " +
			"The quote defaults to the last fully quoted line before the verdict, else the reply's first sentence; --quote overrides it. " +
			"--critic \"<line>\" appends the critic's findings line instead. Words follow the project's documentation language.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGauntletRecord(cmd, dir, args[0], verdict, ours, quote, critic)
		},
	}
	cmd.Flags().StringVar(&verdict, "verdict", "", "file with the judge's reply, ending in the verdict line")
	cmd.Flags().StringVar(&ours, "ours", "", "which side is ours: A or B")
	cmd.Flags().StringVar(&quote, "quote", "", "citation to record (default: from the verdict file)")
	cmd.Flags().StringVar(&critic, "critic", "", "record the critic's findings line instead of a round")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runGauntletRecord(cmd *cobra.Command, dir, id, verdictPath, ours, quote, critic string) error {
	round := verdictPath != ""
	if round == (critic != "") || (round && ours == "") || (!round && (ours != "" || quote != "")) {
		return errGauntletRecordMode
	}
	var verdict []byte
	if round {
		var err error
		if verdict, err = os.ReadFile(verdictPath); err != nil {
			return fmt.Errorf("reading --verdict: %w", err)
		}
	}
	ctx, err := resolvePaths(dir) // local edit: the clone is not refreshed, push does the sending
	if err != nil {
		return err
	}
	slots, _ := filepath.Glob(filepath.Join(ctx.projDir, ".docs", "current", "features", id+"-*", "result.md"))
	if len(slots) != 1 {
		return fmt.Errorf("%s: %w — run 'cerne pull' (and 'cerne result draft %s' if there is no result yet)", id, errGauntletNotMaterialized, id)
	}
	cur, err := os.ReadFile(slots[0])
	if err != nil {
		return err
	}
	lang := store.DocsLanguage(ctx.repoDir)
	var out, what string
	if round {
		var n int
		var outcome string
		if out, n, outcome, err = gauntlet.AddRound(string(cur), lang, string(verdict), ours, quote); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		what = fmt.Sprintf("round %d — %s", n, outcome)
	} else {
		if out, err = gauntlet.AddCritic(string(cur), lang, critic); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		what = "critic findings"
	}
	if err := os.WriteFile(slots[0], []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "cerne: recorded %s in %s result; run 'cerne push' to send it\n", what, id)
	return nil
}
