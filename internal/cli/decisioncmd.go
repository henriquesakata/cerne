package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
)

func newDecisionCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "decision",
		Short: "Create and manage decisions (ADRs) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newDecisionNewCmd(&dir))
	cmd.AddCommand(newDecisionSubstituirCmd(&dir))
	return cmd
}

func newDecisionNewCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "new <title>",
		Short: "Create a decision in active state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionNew(cmd, *dir, args[0])
		},
	}
}

func runDecisionNew(cmd *cobra.Command, dir, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("o título da decisão não pode ser vazio")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	d, err := app.CreateDecision(ctx.repoDir, ctx.projDir, title)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (active). Próximo passo: escreva o ADR e rode 'cerne pull'.\n", d.ID)
	return nil
}

func newDecisionSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Substituir uma decisão por outra (requer --by e --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id da decisão que substitui")
	cmd.Flags().StringVar(&reason, "reason", "", "motivo da substituição")
	return cmd
}

func runDecisionSubstituir(cmd *cobra.Command, dir, id, by, reason string) error {
	// Validate flags before resolveProject so bad invocations fail fast
	// without touching origin.
	if err := app.ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionDecision(ctx.repoDir, ctx.projDir, id, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}
