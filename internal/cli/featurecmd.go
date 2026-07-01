package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

func newFeatureCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "feature",
		Short: "Create and manage features in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newFeatureNewCmd(&dir))
	cmd.AddCommand(newFeatureListCmd(&dir))
	cmd.AddCommand(newFeatureRetagCmd(&dir))
	cmd.AddCommand(newFeatureReparentCmd(&dir))
	cmd.AddCommand(newFeatureRenameCmd(&dir))
	cmd.AddCommand(newTransitionCmds(&dir)...)
	cmd.AddCommand(newErrataCmd(&dir))
	return cmd
}

func newFeatureNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a feature in candidate state",
		Example: "  # registra a candidata (no quadro) e materializa o slot da spec\n" +
			"  cerne feature new \"<título>\" --module <m> [--parent F-NNNN] [--facet code --component gitback]\n" +
			"  cerne pull\n" + lifecycleSeq,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureNew(cmd, *dir, args[0], module, parent, facet, component)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the feature belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (the epic this one decomposes)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; conjunto fechado: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsistema/ferramenta, ex.: gitback,materialize)")
	return cmd
}

func runFeatureNew(cmd *cobra.Command, dir, title, module, parent, facet, component string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.CreateFeature(ctx.repoDir, ctx.projDir, title, module, parent,
		splitInforms(facet), splitInforms(component))
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (candidate). Próximo passo: escreva a spec e rode 'cerne pull'.\n", f.ID)
	return nil
}

func newFeatureRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Redefinir os tags (facet/componente) de uma feature",
		Long: "Define ou substitui os eixos de tag de uma feature, sem tocar no ciclo de vida — preserva estado, " +
			"módulo, épico e docs. Altera só o eixo passado: 'retag F-0001 --component gitback' mantém os facets; " +
			"'--facet \"\"' limpa os facets. Facet fora do conjunto fechado (infra,code,data,process,auth) é rejeitada. " +
			"Vale para feature viva ou implementada; feature encerrada (superseded/rejected) é história congelada.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"))
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "novos facets (CSV; conjunto fechado) — vazio limpa")
	cmd.Flags().StringVar(&component, "component", "", "novos componentes (CSV) — vazio limpa")
	return cmd
}

func runFeatureRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("informe ao menos --facet ou --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.RetagFeature(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retaggeada — facets: [%s] · components: [%s]\n",
		f.ID, strings.Join(f.Facets, ", "), strings.Join(f.Components, ", "))
	return nil
}
