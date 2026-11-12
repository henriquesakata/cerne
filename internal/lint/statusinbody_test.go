package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestStatusInBodyRule (F-0338 crit. 13): a line opening with **Status:** or
// **Estado:** in the spec or plan of a candidate, approved or in-development
// feature is an advisory finding naming the document and the line, and saying
// the state is the card's; the closed states stay silent (their text is
// frozen), and so do a plain Status:, a mid-line mention and fenced code.
func TestStatusInBodyRule(t *testing.T) {
	const body = "# t\n\n"
	lines := []string{"**Status:** Rascunho (2026-10-19)", "**Estado:** em revisão", "**STATUS:** draft"}
	silent := []string{
		"Status: Rascunho",
		"Texto que cita **Status:** no meio.",
		"```\n**Status:** dentro de código\n```",
	}
	cases := []struct {
		state domain.FeatureState
		fires bool
	}{
		{domain.StateCandidate, true},
		{domain.StateApproved, true},
		{domain.StateInDevelopment, true},
		{domain.StateImplemented, false},
		{domain.StateDone, false},
		{domain.StateRejected, false},
		{domain.StateSuperseded, false},
	}
	for _, tc := range cases {
		for _, doc := range []string{"spec", "plan"} {
			for _, l := range append(slices.Clone(lines), silent...) {
				t.Run(string(tc.state)+"/"+doc+"/"+l, func(t *testing.T) {
					dir := t.TempDir()
					if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "t", State: tc.state, Facets: []string{"code"}}); err != nil {
						t.Fatal(err)
					}
					rel := "content/features/F-0001/" + doc + ".md"
					writeDoc(t, dir, rel, body+l+"\n\ntexto\n")
					got, err := Run(dir)
					if err != nil {
						t.Fatal(err)
					}
					var hits []Finding
					for _, f := range got {
						if f.Rule == RuleStatusInBody {
							hits = append(hits, f)
						}
					}
					if !tc.fires || !slices.Contains(lines, l) {
						if len(hits) != 0 {
							t.Fatalf("want no finding, got %v", hits)
						}
						return
					}
					if len(hits) != 1 || hits[0].Entity != "F-0001" || hits[0].File != rel {
						t.Fatalf("want one finding on %s, got %v", rel, hits)
					}
					for _, s := range []string{doc + " line 3", "line 1 of 'cerne show F-0001'"} {
						if !strings.Contains(hits[0].Detail, s) {
							t.Fatalf("detail %q lacks %q", hits[0].Detail, s)
						}
					}
				})
			}
		}
	}
	if !Advisory(RuleStatusInBody) {
		t.Fatal("status-in-body must be advisory")
	}
	if !slices.Contains(RuleOrder, RuleStatusInBody) {
		t.Fatal("RuleStatusInBody missing from RuleOrder")
	}
	if _, ok := ruleRank[RuleStatusInBody]; !ok {
		t.Fatal("RuleStatusInBody missing from ruleRank")
	}
}
