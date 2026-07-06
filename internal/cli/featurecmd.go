package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/spectmpl"
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
	cmd.AddCommand(newFeatureAcceptCmd(&dir))
	cmd.AddCommand(newErrataCmd(&dir))
	return cmd
}

// errFeatureContextRequired is the hard creation gate (F-0120): a candidate
// born as title-only loses its creation context the moment the session that
// minted it is gone, so the CLI refuses it and teaches the minimal path.
var errFeatureContextRequired = errors.New(
	"feature sem contexto: informe ao menos --context \"<1-3 frases do porquê>\", ou a spec completa via --file <spec.md>/stdin")

func newFeatureNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component, file, context string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a feature in candidate state",
		Long: "Cria a candidata já com o contexto capturado: --file/stdin traz a spec completa (one-shot, " +
			"nada a preencher depois) e --context semeia a seção Problema do rascunho com o porquê da criação. " +
			"Sem um dos dois a criação é recusada — o título sozinho não sobrevive à sessão que o criou.",
		Example: "  # registra a candidata com o porquê capturado (mínimo)\n" +
			"  cerne feature new \"<título>\" --context \"<1-3 frases do porquê>\" [--module <m>] [--parent F-NNNN]\n" +
			"  # ou já com a spec completa (one-shot)\n" +
			"  cerne feature new \"<título>\" --file spec.md\n" +
			"  cerne pull\n" + lifecycleSeq,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureNew(cmd, *dir, args[0], module, parent, facet, component, file, context)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the feature belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (the epic this one decomposes)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; conjunto fechado: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsistema/ferramenta, ex.: gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 frases) semeado na seção Problema da spec")
	return cmd
}

func runFeatureNew(cmd *cobra.Command, dir, title, module, parent, facet, component, file, context string) error {
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	hasBody := strings.TrimSpace(body) != ""
	hasContext := strings.TrimSpace(context) != ""
	if hasBody && hasContext {
		return fmt.Errorf("--file/stdin e --context são mutuamente exclusivos: a spec completa já carrega o contexto")
	}
	if !hasBody && !hasContext {
		return errFeatureContextRequired
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	specBody := body
	if hasContext {
		specBody = spectmpl.StubWithContext(title, context)
	}
	f, err := app.CreateFeatureWithSpec(ctx.repoDir, ctx.projDir, title, module, parent,
		splitInforms(facet), splitInforms(component), specBody)
	if err != nil {
		return err
	}
	if hasBody {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s criada (candidate) com spec — já sincronizada. Rode 'cerne pull' para materializar localmente.\n", f.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (candidate) com o contexto semeado na spec. Próximo passo: complete a spec ('cerne pull' materializa o rascunho).\n", f.ID)
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
