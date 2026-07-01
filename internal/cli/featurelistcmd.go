package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// featureStateOrder is the canonical, stable list of feature states — used to
// validate --state and to render the "válidos: …" hint. Kept in lifecycle order
// so the error message reads sensibly.
var featureStateOrder = []domain.FeatureState{
	domain.StateCandidate,
	domain.StateApproved,
	domain.StateInDevelopment,
	domain.StateImplemented,
	domain.StateSuperseded,
	domain.StateRejected,
}

func newFeatureListCmd(dir *string) *cobra.Command {
	var state, module, parent, facet, component string
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Listar features com filtros (--state/--module/--parent/--facet/--component)",
		Long: "Lista as features lidas direto do backend (store), não da projeção '.docs/' — então enxerga o " +
			"quadro mesmo quando '.docs/' está desatualizado (sem 'cerne pull'). Uma feature por linha: id, estado " +
			"(inglês, o mesmo valor de --state), título e marcadores (módulo, facet/componente, épico, sucessora). " +
			"Por padrão oculta as terminais (superseded/rejected); --all as inclui, e um --state terminal explícito " +
			"também as mostra. Filtros --state/--module/--parent/--facet/--component combinam por E lógico. " +
			"--json para saída estruturada (respeita os filtros).",
		Example: "  cerne feature list                         # tudo menos história (superseded/rejected)\n" +
			"  cerne feature list --state in_development   # só o que está em desenvolvimento\n" +
			"  cerne feature list --module gitback --json  # filtrado por módulo, legível por máquina\n" +
			"  cerne feature list --facet infra            # por natureza (eixo D-0003)\n" +
			"  cerne feature list --component gitback       # por subsistema/ferramenta\n" +
			"  cerne feature list --parent F-0050          # os filhos de um épico\n" +
			"  cerne feature list --all                    # inclui superseded/rejected",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureList(cmd, *dir, state, module, parent, facet, component, all, asJSON)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filtrar por estado exato (candidate|approved|in_development|implemented|superseded|rejected)")
	cmd.Flags().StringVar(&module, "module", "", "filtrar por módulo (ex.: gitback)")
	cmd.Flags().StringVar(&parent, "parent", "", "filtrar pelos filhos deste épico (ex.: F-0050)")
	cmd.Flags().StringVar(&facet, "facet", "", "filtrar por facet (ex.: infra)")
	cmd.Flags().StringVar(&component, "component", "", "filtrar por componente (ex.: gitback)")
	cmd.Flags().BoolVar(&all, "all", false, "incluir features superseded/rejected (história)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "saída JSON legível por máquina")
	return cmd
}

func runFeatureList(cmd *cobra.Command, dir, state, module, parent, facet, component string, all, asJSON bool) error {
	// Normalize and validate --state up front so a typo ('dev') fails loudly
	// instead of silently listing nothing.
	stateQ := domain.FeatureState(strings.ToLower(strings.TrimSpace(state)))
	if stateQ != "" && !validFeatureState(stateQ) {
		return fmt.Errorf("estado inválido: %q (válidos: %s)", state, joinFeatureStates())
	}
	moduleQ := strings.ToLower(strings.TrimSpace(module))
	parentQ := strings.ToUpper(strings.TrimSpace(parent))
	// Normalize tag filters to the stored form (facet lowered, component slugged)
	// so '--facet Infra'/'--component Gitback' match what's persisted.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := slug.Make(component)

	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}

	shown := make([]store.Feature, 0, len(features))
	for _, f := range features {
		switch {
		case stateQ != "":
			if f.State != stateQ {
				continue
			}
		case !all:
			// Default view hides terminal history; an explicit --state (above)
			// overrides this, so a caller can still ask for superseded/rejected.
			if f.State == domain.StateSuperseded || f.State == domain.StateRejected {
				continue
			}
		}
		if moduleQ != "" && strings.ToLower(f.Module) != moduleQ {
			continue
		}
		if parentQ != "" && strings.ToUpper(f.Parent) != parentQ {
			continue
		}
		if facetQ != "" && !slices.Contains(f.Facets, facetQ) {
			continue
		}
		if compQ != "" && !slices.Contains(f.Components, compQ) {
			continue
		}
		shown = append(shown, f)
	}

	w := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Features []store.Feature `json:"features"`
		}{shown})
	}

	if len(shown) == 0 {
		noFilter := state == "" && module == "" && parent == "" && facet == "" && component == "" && !all
		if noFilter && len(features) == 0 {
			fmt.Fprintln(w, "Nenhuma feature ainda. Crie a primeira com 'cerne feature new'.")
		} else {
			fmt.Fprintln(w, "Nenhuma feature corresponde aos filtros.")
		}
		return nil
	}

	for _, f := range shown {
		fmt.Fprintln(w, featureListLine(f))
	}
	return nil
}

// featureListLine renders one flat row: id, English state, title, then the
// available markers (módulo / facet / comp / épico / successor), matching the
// `note list` style.
func featureListLine(f store.Feature) string {
	line := fmt.Sprintf("%s  [%s]  %s", f.ID, f.State, f.Title)
	var marks []string
	if f.Module != "" {
		marks = append(marks, "módulo "+f.Module)
	}
	if len(f.Facets) > 0 {
		marks = append(marks, "facet "+strings.Join(f.Facets, ","))
	}
	if len(f.Components) > 0 {
		marks = append(marks, "comp "+strings.Join(f.Components, ","))
	}
	if f.Parent != "" {
		marks = append(marks, "épico "+f.Parent)
	}
	if f.SupersededBy != "" {
		marks = append(marks, "→ "+f.SupersededBy)
	}
	if len(marks) > 0 {
		line += "  · " + strings.Join(marks, " · ")
	}
	return line
}

func validFeatureState(s domain.FeatureState) bool {
	for _, v := range featureStateOrder {
		if v == s {
			return true
		}
	}
	return false
}

func joinFeatureStates() string {
	parts := make([]string, len(featureStateOrder))
	for i, s := range featureStateOrder {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
