package lint

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestSpecMissingBarRule (F-0308 crit. 4): a candidate whose written spec has
// no valid bar is flagged before anyone tries the approve; a valid bar, a stub
// spec (incomplete-doc's business) or a feature past candidacy is not.
func TestSpecMissingBarRule(t *testing.T) {
	cases := []struct {
		name       string
		state      domain.FeatureState
		spec       string
		wantDetail string // "" = no finding
	}{
		{"candidate without bar flags", domain.StateCandidate, "# t\n\n## Problema\n\nProblema real.\n", `no "Barra:" line`},
		{"candidate with malformed bar flags", domain.StateCandidate, "# t\n\nProblema real.\n\nBarra: não aplicável\n", "justification"},
		{"candidate with valid bar is clean", domain.StateCandidate, "# t\n\nProblema real.\n\nBarra: referência nomeada — acesso\n", ""},
		{"stub spec is clean", domain.StateCandidate, "---\ncerne_id: F-0001\n---\n\n" + spectmpl.Stub("t"), ""},
		{"approved without bar is clean", domain.StateApproved, "# t\n\n## Problema\n\nProblema real.\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if err := store.SaveFeature(dir, f); err != nil {
				t.Fatal(err)
			}
			writeDoc(t, dir, "content/features/F-0001/spec.md", tc.spec)

			got, err := Run(dir)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var bar []Finding
			for _, fd := range got {
				if fd.Rule == RuleSpecMissingBar {
					bar = append(bar, fd)
				}
			}
			if tc.wantDetail == "" {
				if len(bar) != 0 {
					t.Fatalf("want no %s finding, got %v", RuleSpecMissingBar, bar)
				}
				return
			}
			if len(bar) != 1 || bar[0].Entity != "F-0001" || bar[0].File != "content/features/F-0001/spec.md" {
				t.Fatalf("want one finding on F-0001's spec, got %v", bar)
			}
			for _, want := range []string{tc.wantDetail, "cerne feature approve F-0001 --check"} {
				if !strings.Contains(bar[0].Detail, want) {
					t.Fatalf("detail %q lacks %q", bar[0].Detail, want)
				}
			}
		})
	}
}
