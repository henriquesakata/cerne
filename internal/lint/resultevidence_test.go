package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestCriteriaSectionPinned guards the coupling between this rule and the spec
// template: if the canonical section is renamed there, this must fail loudly
// instead of the rule going silent.
func TestCriteriaSectionPinned(t *testing.T) {
	for _, s := range spectmpl.Sections {
		if s == criteriaSection {
			return
		}
	}
	t.Fatalf("criteriaSection %q not found in spectmpl.Sections %v", criteriaSection, spectmpl.Sections)
}

func TestSpecCriteria(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want []int
	}{
		{
			name: "dot and paren numbering",
			spec: "## Problema\n\nx\n\n## Objetivo e critérios de aceitação\n\nintro\n\n1. QUANDO a O SISTEMA DEVE b.\n2) QUANDO c O SISTEMA DEVE d.\n\n## Desenho\n\n1. isto é outra seção\n",
			want: []int{1, 2},
		},
		{
			name: "no criteria section",
			spec: "## Problema\n\n1. numerado fora da seção\n",
			want: nil,
		},
		{
			name: "section without numbered items",
			spec: "## Objetivo e critérios de aceitação\n\nprosa sem lista\n",
			want: nil,
		},
		{
			name: "case-insensitive heading",
			spec: "## OBJETIVO E CRITÉRIOS DE ACEITAÇÃO\n\n1. único critério\n",
			want: []int{1},
		},
		{
			name: "nested and duplicate numbering ignored",
			spec: "## Objetivo e critérios de aceitação\n\n1. critério A\n   1. sub-detalhe\n   2. sub-detalhe\n2. critério B\n2. duplicata some\n",
			want: []int{1, 2},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := specCriteria([]byte(tc.spec))
			if len(got) != len(tc.want) {
				t.Fatalf("specCriteria = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("specCriteria = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestResultMentions(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   []int
	}{
		{name: "single mention", result: "Critério 1 atendido com teste X.", want: []int{1}},
		{name: "accent-insensitive keyword", result: "criterio 2 coberto.", want: []int{2}},
		{name: "range with dash", result: "Critérios 1-3 atendidos.", want: []int{1, 2, 3}},
		{name: "range with a", result: "critérios 2 a 4 cobertos pelo dogfood.", want: []int{2, 3, 4}},
		{name: "comma and e list", result: "Critérios 1, 3 e 5: evidência anexa.", want: []int{1, 3, 5}},
		{name: "bare digit does not match", result: "Foram 3 testes e 2 ajustes.", want: nil},
		{name: "absurd range is capped", result: "critérios 1 a 999999 cobertos.", want: []int{1, 999999}},
		{name: "nfd decomposed accent matches", result: "critério 4 atendido.", want: []int{4}},
		{name: "colon after plural keyword", result: "Critérios: 1, 2 — evidência anexa.", want: []int{1, 2}},
		{name: "singular does not expand prose range", result: "critério 2 a 3 horas de execução.", want: []int{2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resultMentions([]byte(tc.result))
			for _, n := range tc.want {
				if !got[n] {
					t.Fatalf("resultMentions(%q) = %v, want %v mentioned", tc.result, got, n)
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("resultMentions(%q) = %v, want exactly %v", tc.result, got, tc.want)
			}
		})
	}
}

func TestResultEvidenceFindings(t *testing.T) {
	specTwoCriteria := "## Objetivo e critérios de aceitação\n\n1. QUANDO a O SISTEMA DEVE b.\n2. QUANDO c O SISTEMA DEVE d.\n"
	after := resultEvidenceSince.Add(24 * time.Hour)
	before := resultEvidenceSince.Add(-24 * time.Hour)

	cases := []struct {
		name        string
		state       domain.FeatureState
		completedAt time.Time
		spec        string
		result      string
		wantDetail  string // "" = no finding
	}{
		{
			name: "all criteria evidenced", state: domain.StateImplemented, completedAt: after,
			spec: specTwoCriteria, result: "Critério 1: teste A. Critério 2: teste B.",
		},
		{
			name: "missing one criterion names it", state: domain.StateImplemented, completedAt: after,
			spec: specTwoCriteria, result: "Critério 1: teste A.",
			wantDetail: "critério(s) 2",
		},
		{
			name: "spec without numbered criteria is silent", state: domain.StateDone, completedAt: after,
			spec: "## Objetivo e critérios de aceitação\n\nprosa livre\n", result: "nada",
		},
		{
			name: "completed before cutoff is grandfathered", state: domain.StateImplemented, completedAt: before,
			spec: specTwoCriteria, result: "sem evidência nenhuma",
		},
		{
			name: "live states are ignored", state: domain.StateInDevelopment, completedAt: after,
			spec: specTwoCriteria, result: "sem evidência",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if err := store.SaveFeature(dir, f); err != nil {
				t.Fatal(err)
			}
			writeDoc(t, dir, "content/features/F-0001/spec.md", tc.spec)
			writeDoc(t, dir, "content/features/F-0001/result.md", tc.result)
			events := []domain.Event{{EntityID: "F-0001", Type: "feature.completed", At: tc.completedAt}}

			got := resultEvidenceFindings(dir, []store.Feature{f}, events)
			if tc.wantDetail == "" {
				if len(got) != 0 {
					t.Fatalf("want no findings, got %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want 1 finding, got %v", got)
			}
			if got[0].Rule != RuleResultMissingEvidence || got[0].Entity != "F-0001" {
				t.Fatalf("finding = %+v", got[0])
			}
			if got[0].File != "content/features/F-0001/result.md" {
				t.Fatalf("finding file = %q", got[0].File)
			}
			if !strings.Contains(got[0].Detail, tc.wantDetail) {
				t.Fatalf("detail %q does not contain %q", got[0].Detail, tc.wantDetail)
			}
		})
	}
}
