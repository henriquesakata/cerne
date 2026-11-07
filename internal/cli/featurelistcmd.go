package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// priorityRank orders domain.ValidPriorities (p0..p3) by index; a missing or
// unrecognized priority sorts last, so unprioritized features never jump the
// queue (F-0204).
func priorityRank(p string) int {
	if i := slices.Index(domain.ValidPriorities, p); i >= 0 {
		return i
	}
	return len(domain.ValidPriorities)
}

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
	var state, module, parent, facet, component, owner, sort string
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List features with filters (--state/--module/--parent/--facet/--component/--owner)",
		Long: "Lists features read straight from the backend (store), not the '.docs/' projection — so it sees the " +
			"board even when '.docs/' is stale (no 'cerne pull'). One feature per line: id, state " +
			"(English, the same value as --state), title and markers (module, facet/component, epic, successor, " +
			"owner/priority). By default hides the terminals (superseded/rejected); --all includes them, and an " +
			"explicit terminal --state shows them too. Filters --state/--module/--parent/--facet/--component/--owner " +
			"combine with logical AND. --sort priority orders p0..p3 first, unset priority last. --json for " +
			"structured output (honors the filters and the sort).",
		Example: "  cerne feature list                         # everything but history (superseded/rejected)\n" +
			"  cerne feature list --state in_development   # only what is in development\n" +
			"  cerne feature list --module gitback --json  # filtered by module, machine-readable\n" +
			"  cerne feature list --facet infra            # by nature (axis D-0003)\n" +
			"  cerne feature list --component gitback       # by subsystem/tool\n" +
			"  cerne feature list --parent F-0050          # the children of an epic\n" +
			"  cerne feature list --owner ana              # filtered by responsible (F-0204)\n" +
			"  cerne feature list --sort priority          # p0 first, unset last (F-0204)\n" +
			"  cerne feature list --all                    # includes superseded/rejected",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureList(cmd, *dir, state, module, parent, facet, component, owner, sort, all, asJSON)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by exact state (candidate|approved|in_development|implemented|done|superseded|rejected)")
	cmd.Flags().StringVar(&module, "module", "", "filter by module (e.g. gitback)")
	cmd.Flags().StringVar(&parent, "parent", "", "filter by the children of this epic (e.g. F-0050)")
	cmd.Flags().StringVar(&facet, "facet", "", "filter by facet (e.g. infra)")
	cmd.Flags().StringVar(&component, "component", "", "filter by component (e.g. gitback)")
	cmd.Flags().StringVar(&owner, "owner", "", "filter by owner (e.g. ana, F-0204)")
	cmd.Flags().StringVar(&sort, "sort", "", "sort order: priority (p0..p3 first, unset last, F-0204)")
	cmd.Flags().BoolVar(&all, "all", false, "include superseded/rejected features (history)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runFeatureList(cmd *cobra.Command, dir, state, module, parent, facet, component, owner, sortBy string, all, asJSON bool) error {
	if sortBy != "" && sortBy != "priority" {
		return fmt.Errorf("invalid --sort: %q (valid: priority)", sortBy)
	}
	// Normalize and validate --state up front so a typo ('dev') fails loudly
	// instead of silently listing nothing.
	stateQ := domain.FeatureState(strings.ToLower(strings.TrimSpace(state)))
	if stateQ != "" && !validFeatureState(stateQ) {
		return fmt.Errorf("invalid state: %q (valid: %s)", state, joinFeatureStates())
	}
	moduleQ := strings.ToLower(strings.TrimSpace(module))
	// --parent is a gate as much as a filter: a blank or unknown epic fails
	// loudly instead of listing nothing (F-0326), like --state above.
	parentQ := domain.CanonicalID(parent)
	if cmd.Flags().Changed("parent") && parentQ == "" {
		return fmt.Errorf("--parent needs a feature id")
	}
	// Normalize tag filters to the stored form (facet lowered, component slugged)
	// so '--facet Infra'/'--component Gitback' match what's persisted.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := slug.Make(component)
	ownerQ := strings.ToLower(strings.TrimSpace(owner))

	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}
	if parentQ != "" && !slices.ContainsFunc(features, func(f store.Feature) bool { return f.ID == parentQ }) {
		return fmt.Errorf("feature %s not found in backend", parentQ)
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
		if parentQ != "" && f.Parent != parentQ { // the store loads parents canonical
			continue
		}
		if facetQ != "" && !slices.Contains(f.Facets, facetQ) {
			continue
		}
		if compQ != "" && !slices.Contains(f.Components, compQ) {
			continue
		}
		if ownerQ != "" && strings.ToLower(f.Owner) != ownerQ {
			continue
		}
		shown = append(shown, f)
	}

	if sortBy == "priority" {
		sort.SliceStable(shown, func(i, j int) bool {
			return priorityRank(shown[i].Priority) < priorityRank(shown[j].Priority)
		})
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
		noFilter := state == "" && module == "" && parent == "" && facet == "" && component == "" && owner == "" && !all
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
	if f.Owner != "" {
		marks = append(marks, "owner "+f.Owner)
	}
	if f.Priority != "" {
		marks = append(marks, "priority "+f.Priority)
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
