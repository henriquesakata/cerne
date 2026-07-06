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
// validate --state and to render the "valid: …" hint. Kept in lifecycle order
// so the error message reads sensibly.
var featureStateOrder = []domain.FeatureState{
	domain.StateCandidate,
	domain.StateApproved,
	domain.StateInDevelopment,
	domain.StateImplemented,
	domain.StateDone,
	domain.StateSuperseded,
	domain.StateRejected,
}

func newFeatureListCmd(dir *string) *cobra.Command {
	var state, module, parent, facet, component string
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List features with filters (--state/--module/--parent/--facet/--component)",
		Long: "Lists features read straight from the backend (store), not the '.docs/' projection — so it sees the " +
			"board even when '.docs/' is stale (no 'cerne pull'). One feature per line: id, state " +
			"(English, the same value as --state), title and markers (module, facet/component, epic, successor). " +
			"By default hides the terminals (superseded/rejected); --all includes them, and an explicit terminal --state " +
			"shows them too. Filters --state/--module/--parent/--facet/--component combine with logical AND. " +
			"--json for structured output (honors the filters).",
		Example: "  cerne feature list                         # everything but history (superseded/rejected)\n" +
			"  cerne feature list --state in_development   # only what is in development\n" +
			"  cerne feature list --module gitback --json  # filtered by module, machine-readable\n" +
			"  cerne feature list --facet infra            # by nature (axis D-0003)\n" +
			"  cerne feature list --component gitback       # by subsystem/tool\n" +
			"  cerne feature list --parent F-0050          # the children of an epic\n" +
			"  cerne feature list --all                    # includes superseded/rejected",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureList(cmd, *dir, state, module, parent, facet, component, all, asJSON)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by exact state (candidate|approved|in_development|implemented|done|superseded|rejected)")
	cmd.Flags().StringVar(&module, "module", "", "filter by module (e.g. gitback)")
	cmd.Flags().StringVar(&parent, "parent", "", "filter by the children of this epic (e.g. F-0050)")
	cmd.Flags().StringVar(&facet, "facet", "", "filter by facet (e.g. infra)")
	cmd.Flags().StringVar(&component, "component", "", "filter by component (e.g. gitback)")
	cmd.Flags().BoolVar(&all, "all", false, "include superseded/rejected features (history)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runFeatureList(cmd *cobra.Command, dir, state, module, parent, facet, component string, all, asJSON bool) error {
	// Normalize and validate --state up front so a typo ('dev') fails loudly
	// instead of silently listing nothing.
	stateQ := domain.FeatureState(strings.ToLower(strings.TrimSpace(state)))
	if stateQ != "" && !validFeatureState(stateQ) {
		return fmt.Errorf("invalid state: %q (valid: %s)", state, joinFeatureStates())
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
			fmt.Fprintln(w, "No features yet. Create the first one with 'cerne feature new'.")
		} else {
			fmt.Fprintln(w, "No features match the filters.")
		}
		return nil
	}

	for _, f := range shown {
		fmt.Fprintln(w, featureListLine(f))
	}
	return nil
}

// featureListLine renders one flat row: id, English state, title, then the
// available markers (module / facet / comp / epic / successor), matching the
// `note list` style.
func featureListLine(f store.Feature) string {
	line := fmt.Sprintf("%s  [%s]  %s", f.ID, f.State, f.Title)
	var marks []string
	if f.Module != "" {
		marks = append(marks, "module "+f.Module)
	}
	if len(f.Facets) > 0 {
		marks = append(marks, "facet "+strings.Join(f.Facets, ","))
	}
	if len(f.Components) > 0 {
		marks = append(marks, "comp "+strings.Join(f.Components, ","))
	}
	if f.Parent != "" {
		marks = append(marks, "epic "+f.Parent)
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
