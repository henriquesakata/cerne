package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

func newClassifyCmd() *cobra.Command {
	var dir, module string
	var yes bool
	cmd := &cobra.Command{
		Use:   "classify [id]",
		Short: "Assign a module to module-less features",
		Long: "Sem argumentos, faz backfill heurístico: atribui módulo às features sem " +
			"módulo quando exatamente um módulo declarado casa (dry-run por padrão; --yes aplica). " +
			"Com um id e --module, atribui o módulo a uma feature manualmente.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runClassifyManual(cmd, dir, args[0], module)
			}
			return runClassifyBackfill(cmd, dir, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "apply the heuristic backfill instead of printing the dry-run plan")
	cmd.Flags().StringVar(&module, "module", "", "module to assign in manual mode (cerne classify <id> --module <m>)")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runClassifyBackfill(cmd *cobra.Command, dir string, yes bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	assigned, skipped, err := app.ClassifyBackfill(ctx.repoDir, ctx.projDir, yes)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(assigned) == 0 {
		fmt.Fprintln(out, "Nada a classificar.")
		return nil
	}
	sort.Slice(assigned, func(i, j int) bool { return assigned[i].ID < assigned[j].ID })
	for _, a := range assigned {
		fmt.Fprintf(out, "%s → %s\n", a.ID, a.Module)
	}
	if yes {
		fmt.Fprintf(out, "Classificadas %d feature(s).\n", len(assigned))
		if skipped > 0 {
			fmt.Fprintf(out, "%d feature(s) sem módulo permanecem (transversais/ambíguas).\n", skipped)
		}
		return nil
	}
	fmt.Fprintf(out, "%d feature(s) sem módulo permanecem (transversais/ambíguas).\n", skipped)
	fmt.Fprintln(out, "Rode com --yes para aplicar.")
	return nil
}

func runClassifyManual(cmd *cobra.Command, dir, id, module string) error {
	if module == "" {
		return fmt.Errorf("--module é obrigatório na atribuição manual (cerne classify %s --module <m>)", id)
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if err := app.ClassifyFeatureManually(ctx.repoDir, ctx.projDir, id, module); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s → %s.\n", id, module)
	return nil
}
