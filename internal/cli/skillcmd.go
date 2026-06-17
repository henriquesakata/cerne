package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/skills"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install cerne's Claude Code skills into a project",
	}
	cmd.AddCommand(newSkillInstallCmd())
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

// skillStatusLabel maps an install outcome to its pt-BR label.
var skillStatusLabel = map[skills.Status]string{
	skills.StatusCreated:   "criada",
	skills.StatusUpdated:   "atualizada",
	skills.StatusUnchanged: "já atual",
}

func runSkillInstall(cmd *cobra.Command, dir string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	results, err := skills.Install(dir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, r := range results {
		label := skillStatusLabel[r.Status]
		if label == "" {
			label = string(r.Status)
		}
		fmt.Fprintf(out, "%s: %s em .claude/skills/%s\n", r.Skill, label, r.Skill)
	}
	return nil
}
