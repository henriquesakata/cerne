package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

func newReferenceCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "reference",
		Short: "Register and manage references in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newReferenceNewCmd(&dir))
	cmd.AddCommand(newReferenceListCmd(&dir))
	cmd.AddCommand(newReferenceRetagCmd(&dir))
	return cmd
}

func newReferenceRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Redefinir os tags (facet/componente) de uma referência current",
		Long: "Define ou substitui os eixos de tag de uma referência current, sem torná-la obsolete — preserva url, " +
			"informs e corpo. Altera só o eixo passado: 'retag R-0001 --component gitea' mantém os facets; '--facet \"\"' " +
			"limpa os facets. Facet fora do conjunto fechado (infra,code,data,process,auth) é rejeitada.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"))
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "novos facets (CSV; conjunto fechado) — vazio limpa")
	cmd.Flags().StringVar(&component, "component", "", "novos componentes (CSV) — vazio limpa")
	return cmd
}

func runReferenceRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("informe ao menos --facet ou --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	r, err := app.RetagReference(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retaggeada — facets: [%s] · components: [%s]\n",
		r.ID, strings.Join(r.Facets, ", "), strings.Join(r.Components, ", "))
	return nil
}

func newReferenceNewCmd(dir *string) *cobra.Command {
	var url, informs, file, facet, component string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Register an external source (article/doc) as a reference",
		Long: "Registra um artigo/documento externo como referência (marca externa + URL). " +
			"O conteúdo (texto/resumo) vem de --file ou da entrada padrão e entra no índice, " +
			"então o 'ask' passa a citá-lo. Use --informs para ligar a fonte às entidades " +
			"(F-/D-/RB-/N-) que ela motivou. Use --facet (eixo fechado: infra,code,data,process,auth) e " +
			"--component (subsistema/ferramenta, ex.: gitea,kpack) para categorizar — ambos multivalor, " +
			"separados por vírgula.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceNew(cmd, *dir, args[0], url, informs, file, facet, component)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "source URL (required)")
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/D-/RB-/N- ids this source motivated")
	cmd.Flags().StringVar(&file, "file", "", "file with the source content (defaults to stdin)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (comma-separated; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (comma-separated subsystem/tool tags, e.g. gitea,kpack)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func runReferenceNew(cmd *cobra.Command, dir, title, url, informs, file, facet, component string) error {
	content, err := readReferenceContent(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	r, err := app.CreateExternalReference(ctx.repoDir, ctx.projDir, title, url, content, splitInforms(informs), splitInforms(facet), splitInforms(component))
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s registrada (externa: %s).\n", r.ID, r.URL)
	return nil
}

func newReferenceListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var facet, component string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Listar referências — current por padrão; --all inclui obsoletas",
		Long: "Lista as referências lidas direto do backend (store). Por padrão mostra só as " +
			"current; --all inclui as obsolete. Cada linha traz id, estado, título e marcadores " +
			"(externa, facet/componente, informa F-/D-, URL). Filtre por --facet e/ou --component " +
			"(E lógico). --json para saída estruturada.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceList(cmd, *dir, all, asJSON, facet, component)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "incluir referências obsolete")
	cmd.Flags().BoolVar(&asJSON, "json", false, "saída JSON legível por máquina")
	cmd.Flags().StringVar(&facet, "facet", "", "filtrar por facet (ex.: infra)")
	cmd.Flags().StringVar(&component, "component", "", "filtrar por componente (ex.: gitea)")
	return cmd
}

func runReferenceList(cmd *cobra.Command, dir string, all, asJSON bool, facet, component string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	refs, err := store.LoadAllReferences(ctx.repoDir)
	if err != nil {
		return err
	}
	// Normalize the filters to the stored form so '--facet Infra'/'--component
	// Gitea' match the lower/slug values persisted on the reference.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := slug.Make(component)
	w := cmd.OutOrStdout()
	shown := make([]store.Reference, 0, len(refs))
	for _, r := range refs {
		if !all && r.State != domain.ReferenceCurrent {
			continue
		}
		if facetQ != "" && !slices.Contains(r.Facets, facetQ) {
			continue
		}
		if compQ != "" && !slices.Contains(r.Components, compQ) {
			continue
		}
		shown = append(shown, r)
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			References []store.Reference `json:"references"`
		}{shown})
	}

	for _, r := range shown {
		line := fmt.Sprintf("%s  [%s]  %s", r.ID, r.State, r.Title)
		var tags []string
		if r.External {
			tags = append(tags, "externa")
		}
		if len(r.Facets) > 0 {
			tags = append(tags, "facet "+strings.Join(r.Facets, ","))
		}
		if len(r.Components) > 0 {
			tags = append(tags, "comp "+strings.Join(r.Components, ","))
		}
		if len(r.Informs) > 0 {
			tags = append(tags, "informa "+strings.Join(r.Informs, ","))
		}
		if r.URL != "" {
			tags = append(tags, r.URL)
		}
		if len(tags) > 0 {
			line += "  · " + strings.Join(tags, " · ")
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
		if all {
			fmt.Fprintln(w, "Nenhuma referência registrada. Use 'cerne reference new \"<título>\" --url <url>' para registrar.")
		} else {
			fmt.Fprintln(w, "Nenhuma referência current. Use 'cerne reference list --all' para ver as obsolete.")
		}
	}
	return nil
}

// readReferenceContent reads the source content from --file, or from stdin when
// no file is given.
func readReferenceContent(cmd *cobra.Command, file string) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("--file: %w", err)
		}
		return string(data), nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// splitInforms parses the comma-separated --informs flag into trimmed ids
// (empties dropped); app.CreateExternalReference validates them.
func splitInforms(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
