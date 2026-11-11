package lint

import (
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func feat(id, module string, state domain.FeatureState) store.Feature {
	return store.Feature{ID: id, Module: module, State: state}
}

func featC(id string, state domain.FeatureState, components ...string) store.Feature {
	return store.Feature{ID: id, State: state, Components: components}
}

const baseSHA = "1a2b3c4d5e6f70819293a4b5c6d7e8f901234567"

var coreMap = map[string][]string{
	"core": {"internal/domain/**", "internal/store/**"},
	"cli":  {"internal/cli/**"},
}

func driftEntities(fs []Finding) []string {
	var ids []string
	for _, f := range fs {
		if f.Rule != RuleDriftSuspect {
			t := "non-drift finding leaked"
			panic(t)
		}
		ids = append(ids, f.Entity)
	}
	return ids
}

func TestDriftFindings(t *testing.T) {
	tests := []struct {
		name           string
		changed        []string
		baseline       string
		modulePaths    map[string][]string
		componentPaths map[string][]string
		features       []store.Feature
		wantIDs        []string // entity ids, in output order
	}{
		{
			name:        "crit1 implemented feature in changed module is flagged",
			changed:     []string{"internal/domain/feature.go"},
			baseline:    baseSHA,
			modulePaths: coreMap,
			features:    []store.Feature{feat("F-0001", "core", domain.StateImplemented)},
			wantIDs:     []string{"F-0001"},
		},
		{
			name:        "crit2 nil module map is inert",
			changed:     []string{"internal/domain/feature.go"},
			baseline:    baseSHA,
			modulePaths: nil,
			features:    []store.Feature{feat("F-0001", "core", domain.StateImplemented)},
			wantIDs:     nil,
		},
		{
			name:        "crit2 empty baseline is inert",
			changed:     []string{"internal/domain/feature.go"},
			baseline:    "",
			modulePaths: coreMap,
			features:    []store.Feature{feat("F-0001", "core", domain.StateImplemented)},
			wantIDs:     nil,
		},
		{
			name:        "crit3 active feature in module suppresses drift",
			changed:     []string{"internal/domain/feature.go"},
			baseline:    baseSHA,
			modulePaths: coreMap,
			features: []store.Feature{
				feat("F-0001", "core", domain.StateImplemented),
				feat("F-0002", "core", domain.StateInDevelopment),
			},
			wantIDs: nil,
		},
		{
			name:        "crit4 change outside any glob is ignored",
			changed:     []string{"README.md", "ui/src/app.tsx"},
			baseline:    baseSHA,
			modulePaths: coreMap,
			features:    []store.Feature{feat("F-0001", "core", domain.StateImplemented)},
			wantIDs:     nil,
		},
		{
			name:        "only implemented features flagged, not superseded/rejected",
			changed:     []string{"internal/cli/root.go"},
			baseline:    baseSHA,
			modulePaths: coreMap,
			features: []store.Feature{
				feat("F-0003", "cli", domain.StateImplemented),
				feat("F-0004", "cli", domain.StateSuperseded),
				feat("F-0005", "cli", domain.StateRejected),
			},
			wantIDs: []string{"F-0003"},
		},
		{
			name:           "F-0116 crit1 component match flags implemented feature",
			changed:        []string{"internal/lint/drift.go"},
			baseline:       baseSHA,
			componentPaths: map[string][]string{"drift": {"internal/lint/drift.go", "internal/cli/drift.go"}},
			features:       []store.Feature{featC("F-0010", domain.StateImplemented, "drift", "lint")},
			wantIDs:        []string{"F-0010"},
		},
		{
			name:           "F-0116 crit2 active feature carrying the component suppresses",
			changed:        []string{"internal/lint/drift.go"},
			baseline:       baseSHA,
			componentPaths: map[string][]string{"drift": {"internal/lint/**"}},
			features: []store.Feature{
				featC("F-0010", domain.StateImplemented, "drift"),
				featC("F-0011", domain.StateInDevelopment, "drift"),
			},
			wantIDs: nil,
		},
		{
			name:           "F-0116 crit3 module and component hit dedupes to one finding",
			changed:        []string{"internal/cli/drift.go"},
			baseline:       baseSHA,
			modulePaths:    coreMap, // cli module matches
			componentPaths: map[string][]string{"drift": {"internal/cli/drift.go"}},
			features: []store.Feature{
				{ID: "F-0012", Module: "cli", State: domain.StateImplemented, Components: []string{"drift"}},
			},
			wantIDs: []string{"F-0012"},
		},
		{
			name:           "F-0116 crit4 component axis alone works without module_paths",
			changed:        []string{"internal/gitback/gitback.go"},
			baseline:       baseSHA,
			componentPaths: map[string][]string{"gitback": {"internal/gitback/**"}},
			features: []store.Feature{
				featC("F-0013", domain.StateImplemented, "gitback"),
				featC("F-0014", domain.StateImplemented, "llm"), // other component untouched
			},
			wantIDs: []string{"F-0013"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DriftFindings(tc.changed, tc.baseline, tc.modulePaths, tc.componentPaths, tc.features)
			if !reflect.DeepEqual(driftEntities(got), tc.wantIDs) {
				t.Fatalf("entities = %v, want %v", driftEntities(got), tc.wantIDs)
			}
		})
	}
}

func TestDriftDetailCitesBaselineAndTruncates(t *testing.T) {
	changed := []string{
		"internal/domain/a.go", "internal/domain/b.go", "internal/domain/c.go",
		"internal/domain/d.go", "internal/domain/e.go", "internal/domain/f.go",
		"internal/domain/g.go",
	}
	got := DriftFindings(changed, baseSHA, coreMap, nil, []store.Feature{feat("F-0001", "core", domain.StateImplemented)})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	d := got[0].Detail
	if !strings.Contains(d, baseSHA[:7]) {
		t.Fatalf("detail %q must cite baseline %q", d, baseSHA[:7])
	}
	if !strings.Contains(d, "(+2 more)") { // 7 changed, 5 shown
		t.Fatalf("detail %q must truncate to 5 + remainder", d)
	}
}

func TestDriftDeterministic(t *testing.T) {
	feats := []store.Feature{
		feat("F-0002", "core", domain.StateImplemented),
		feat("F-0001", "core", domain.StateImplemented),
	}
	changed := []string{"internal/store/x.go", "internal/domain/y.go"}
	a := DriftFindings(changed, baseSHA, coreMap, nil, feats)
	b := DriftFindings(changed, baseSHA, coreMap, nil, feats)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("non-deterministic: %v vs %v", a, b)
	}
	Sort(a) // exported canonical sort must not panic and keep drift findings
	if len(a) != 2 {
		t.Fatalf("want 2 findings, got %d", len(a))
	}
}
