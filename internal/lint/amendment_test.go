package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestAppendedAmendmentRule (F-0338 crit. 10): a level 2-4 heading opening
// with Emenda/Emendas/Amendment/Amendments in the spec or plan of an approved
// or in-development feature is an advisory finding naming the document, the
// line and the amend; candidate and the closed states stay silent, and so do
// prose, other levels, longer words and headings inside fenced code.
func TestAppendedAmendmentRule(t *testing.T) {
	const body = "# t\n\n## Objetivo\n\n1. critério\n\n"
	headings := []string{"## Emenda (2026-11-10)", "### Emenda de prefixo", "## Emendas da auditoria (W-1)", "## Amendment", "#### amendments"}
	silent := []string{
		"A emenda entrou no texto do critério.",
		"# Emenda",
		"##### Emenda",
		"## Emendado",
		"```\n## Emenda dentro de código\n```",
	}
	cases := []struct {
		state domain.FeatureState
		fires bool
	}{
		{domain.StateCandidate, false},
		{domain.StateApproved, true},
		{domain.StateInDevelopment, true},
		{domain.StateImplemented, false},
		{domain.StateDone, false},
		{domain.StateRejected, false},
		{domain.StateSuperseded, false},
	}
	for _, tc := range cases {
		for _, doc := range []string{"spec", "plan"} {
			for _, h := range append(slices.Clone(headings), silent...) {
				t.Run(string(tc.state)+"/"+doc+"/"+h, func(t *testing.T) {
					dir := t.TempDir()
					if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "t", State: tc.state, Facets: []string{"code"}}); err != nil {
						t.Fatal(err)
					}
					rel := "content/features/F-0001/" + doc + ".md"
					writeDoc(t, dir, rel, body+h+"\n\ntexto\n")
					got, err := Run(dir)
					if err != nil {
						t.Fatal(err)
					}
					var hits []Finding
					for _, f := range got {
						if f.Rule == RuleAppendedAmendment {
							hits = append(hits, f)
						}
					}
					want := tc.fires && slices.Contains(headings, h)
					if !want {
						if len(hits) != 0 {
							t.Fatalf("want no finding, got %v", hits)
						}
						return
					}
					if len(hits) != 1 || hits[0].Entity != "F-0001" || hits[0].File != rel {
						t.Fatalf("want one finding on %s, got %v", rel, hits)
					}
					for _, s := range []string{doc + " line 7", `cerne feature amend F-0001 --doc ` + doc + ` --file <` + doc + `.md> --reason "<why>"`} {
						if !strings.Contains(hits[0].Detail, s) {
							t.Fatalf("detail %q lacks %q", hits[0].Detail, s)
						}
					}
				})
			}
		}
	}
	if !Advisory(RuleAppendedAmendment) {
		t.Fatal("appended-amendment must be advisory")
	}
	if !slices.Contains(RuleOrder, RuleAppendedAmendment) {
		t.Fatal("RuleAppendedAmendment missing from RuleOrder")
	}
	if _, ok := ruleRank[RuleAppendedAmendment]; !ok {
		t.Fatal("RuleAppendedAmendment missing from ruleRank")
	}
}

// TestAmendmentLines: the shared matcher the amend refusal also uses (crit. 5)
// returns the 1-based lines of the headings, outside fenced code only.
func TestAmendmentLines(t *testing.T) {
	text := "# t\n## Emenda A\n~~~\n## Emenda falsa\n~~~\n\n### Amendments B\n"
	if got := AmendmentLines(text); !slices.Equal(got, []int{2, 7}) {
		t.Fatalf("AmendmentLines = %v, want [2 7]", got)
	}
}
