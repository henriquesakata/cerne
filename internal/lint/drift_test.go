package lint

import (
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func feat(id, module string, state domain.FeatureState) store.Feature {
	return store.Feature{ID: id, Module: module, State: state}
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
		name        string
		changed     []string
		baseline    string
		modulePaths map[string][]string
		features    []store.Feature
		wantIDs     []string // entity ids, in output order
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DriftFindings(tc.changed, tc.baseline, tc.modulePaths, tc.features)
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
	got := DriftFindings(changed, baseSHA, coreMap, []store.Feature{feat("F-0001", "core", domain.StateImplemented)})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	d := got[0].Detail
	if !strings.Contains(d, baseSHA[:7]) {
		t.Fatalf("detail %q must cite baseline %q", d, baseSHA[:7])
	}
	if !strings.Contains(d, "(+2 mais)") { // 7 changed, 5 shown
		t.Fatalf("detail %q must truncate to 5 + remainder", d)
	}
}

func TestDriftDeterministic(t *testing.T) {
	feats := []store.Feature{
		feat("F-0002", "core", domain.StateImplemented),
		feat("F-0001", "core", domain.StateImplemented),
	}
	changed := []string{"internal/store/x.go", "internal/domain/y.go"}
	a := DriftFindings(changed, baseSHA, coreMap, feats)
	b := DriftFindings(changed, baseSHA, coreMap, feats)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("non-deterministic: %v vs %v", a, b)
	}
	Sort(a) // exported canonical sort must not panic and keep drift findings
	if len(a) != 2 {
		t.Fatalf("want 2 findings, got %d", len(a))
	}
}
