package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/skills"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install cerne's skills into a project (Claude Code, plus Cursor/Codex/Gemini when detected)",
	}
	cmd.AddCommand(newSkillInstallCmd())
	cmd.AddCommand(newSkillSyncCmd())
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/update the canonical cerne skills under .claude/skills/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillInstall(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	return cmd
}

func newSkillSyncCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Materialize golden-path notes/runbooks as generated agent skills",
		Long:  "Reconciles the learned-* namespace under .claude/skills/ against the active notes and runbooks tagged with the 'golden-path' component: one generated skill per entity, orphans removed. Generated skills are tool-managed — edit the entity in the backend and re-run the sync, never the generated file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillSync(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	return cmd
}

func runSkillSync(cmd *cobra.Command, dir string) error {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	items, err := skills.LoadGoldenPaths(ctx.repoDir)
	if err != nil {
		return err
	}
	results, err := skills.SyncLearned(ctx.projDir, items)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No golden-path entities to materialize (tag a note/runbook with the 'golden-path' component).")
		return nil
	}
	printSkillResults(cmd.OutOrStdout(), results)
	return nil
}

// skillStatusLabel maps an install outcome to its label.
var skillStatusLabel = map[skills.Status]string{
	skills.StatusCreated:   "created",
	skills.StatusUpdated:   "updated",
	skills.StatusUnchanged: "up to date",
}

func runSkillInstall(cmd *cobra.Command, dir string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	det := hooks.DetectProviders(dir)
	results, err := skills.Install(dir, det...)
	if err != nil {
		return err
	}
	printSkillResults(cmd.OutOrStdout(), results)
	printAgentLines(cmd.OutOrStdout(), results, det)
	return nil
}

// printSkillResults writes one status line per installed asset. Shared by
// `cerne skill install` and `cerne init`. A skipped asset prints its manual
// step instead of a location.
func printSkillResults(out io.Writer, results []skills.Result) {
	for _, r := range results {
		label := skillStatusLabel[r.Status]
		if label == "" {
			label = string(r.Status)
		}
		path := r.Path
		if path == "" {
			path = ".claude/skills/" + r.Skill
		}
		if r.Status == skills.StatusSkipped {
			fmt.Fprintf(out, "%s: skipped (%s) — %s\n", r.Skill, path, r.Note)
			continue
		}
		fmt.Fprintf(out, "%s: %s in %s\n", r.Skill, label, path)
	}
}

// printAgentLines writes the per-agent guide summary (every agent, detected or
// not). Shared by `cerne skill install` and `cerne init`.
func printAgentLines(out io.Writer, results []skills.Result, detected []hooks.Provider) {
	for _, l := range skills.AgentLines(results, detected) {
		fmt.Fprintln(out, l)
	}
}
