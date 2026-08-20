package lint

import (
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestResultGauntletFindings(t *testing.T) {
	after := resultGauntletSince.Add(24 * time.Hour)
	before := resultGauntletSince.Add(-24 * time.Hour)

	recorded := "## Resultado\n\nfeito\n\n## Gauntlet\n\nBarra: middleware do finance — apps/finance/web/middleware.ts\nRodada 1 — **vitória**: \"Só A tira do matcher as rotas públicas\"\nAchados do crítico: 2 — corrigidos 1; refutados 1 com file:line\n\n## Débitos\n\nnada\n"
	exempt := "## Resultado\n\nfeito\n\n## Gauntlet\n\nBarra: dispensada por decisão humana (\"segue sem barra dessa vez\")\n"
	emptySection := "## Resultado\n\nfeito\n\n## Gauntlet\n\nrodamos o loop, confia\n"

	cases := []struct {
		name        string
		state       domain.FeatureState
		completedAt time.Time
		result      string
		wantDetail  string // "" = no finding
	}{
		{"verdict recorded is clean", domain.StateImplemented, after, recorded, ""},
		{"exemption recorded is clean", domain.StateImplemented, after, exempt, ""},
		{"nfd verdict is clean", domain.StateDone, after, "## Gauntlet\nBarra: x\nRodada 1 — vitória: “citação”\n", ""},
		{"no section flags", domain.StateImplemented, after, "## Resultado\n\nfeito\n", "sem seção"},
		{"section without cited verdict flags", domain.StateImplemented, after, emptySection, "sem veredito citado"},
		{"verdict without quote flags", domain.StateImplemented, after, "## Gauntlet\nBarra: x\nRodada 1 — vitória\n", "sem veredito citado"},
		{"completed before cutoff is grandfathered", domain.StateImplemented, before, "## Resultado\n\nfeito\n", ""},
		{"live states are ignored", domain.StateInDevelopment, after, "## Resultado\n\nfeito\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if err := store.SaveFeature(dir, f); err != nil {
				t.Fatal(err)
			}
			writeDoc(t, dir, "content/features/F-0001/result.md", tc.result)
			events := []domain.Event{{EntityID: "F-0001", Type: "feature.completed", At: tc.completedAt}}

			got := resultGauntletFindings(dir, []store.Feature{f}, events)
			if tc.wantDetail == "" {
				if len(got) != 0 {
					t.Fatalf("want no findings, got %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want 1 finding, got %v", got)
			}
			if got[0].Rule != RuleResultMissingGauntlet || got[0].Entity != "F-0001" {
				t.Fatalf("finding = %+v", got[0])
			}
			if !strings.Contains(got[0].Detail, tc.wantDetail) {
				t.Fatalf("detail %q lacks %q", got[0].Detail, tc.wantDetail)
			}
		})
	}
}

// A result.md that never materialized counts as zero record, same stance as
// result-missing-evidence: doc absence is missing-gate-doc's finding, but the
// loop still has no proof.
func TestResultGauntletMissingDoc(t *testing.T) {
	dir := t.TempDir()
	f := store.Feature{ID: "F-0001", Title: "t", State: domain.StateImplemented}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatal(err)
	}
	events := []domain.Event{{EntityID: "F-0001", Type: "feature.completed", At: resultGauntletSince.Add(time.Hour)}}
	got := resultGauntletFindings(dir, []store.Feature{f}, events)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "sem seção") {
		t.Fatalf("want one 'sem seção' finding, got %v", got)
	}
}
