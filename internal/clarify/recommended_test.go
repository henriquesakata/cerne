package clarify

import (
	"slices"
	"testing"
)

// TestLegacyMarkersUnchanged pins the markers written before the recommended
// suffix: with or without options, they read exactly as they always did.
// F-0517 crit. 9
func TestLegacyMarkersUnchanged(t *testing.T) {
	tests := []struct {
		content  string
		question string
		options  []string
	}{
		{"[ESCLARECER: qual?]", "qual?", nil},
		{"[ESCLARECER]", "", nil},
		{"[CLARIFY: where? | in-place | sidecar]", "where?", []string{"in-place", "sidecar"}},
		{`[ESCLARECER: onde? \| a \| b]`, "onde?", []string{"a", "b"}},
		{"[ESCLARECER: q? | recomendada pelo time | (recomendada) na frente]", "q?", []string{"recomendada pelo time", "(recomendada) na frente"}},
	}
	for _, tc := range tests {
		got := Parse(tc.content)
		if len(got) != 1 || got[0].Question != tc.question || !slices.Equal(got[0].Options, tc.options) {
			t.Errorf("Parse(%q) = %+v", tc.content, got)
			continue
		}
		if got[0].Recommended != 0 || got[0].ManyRecommended {
			t.Errorf("Parse(%q) marked a recommended option: %+v", tc.content, got[0])
		}
	}
}

// F-0517 crit. 1
func TestParseRecommendedOption(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		options     []string
		recommended int // 1-based, 0 = none
	}{
		{"pt suffix", "[ESCLARECER: q? | A (recomendada) | B]", []string{"A", "B"}, 1},
		{"en suffix", "[CLARIFY: q? | A | B (recommended)]", []string{"A", "B"}, 2},
		{"en suffix in a pt marker", "[ESCLARECER: q? | A (recommended) | B]", []string{"A", "B"}, 1},
		{"pt suffix in an en marker", "[CLARIFY: q? | A | B (recomendada)]", []string{"A", "B"}, 2},
		{"case-insensitive", "[CLARIFY: q? | A (Recommended) | B]", []string{"A", "B"}, 1},
		{"suffix in the middle stays text", "[CLARIFY: q? | A (recommended) plus | B]", []string{"A (recommended) plus", "B"}, 0},
		{"escaped pipes in a table cell", `| [ESCLARECER: q? \| A \| B (recomendada)] |`, []string{"A", "B"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.content)
			if len(got) != 1 {
				t.Fatalf("Parse = %+v", got)
			}
			if !slices.Equal(got[0].Options, tc.options) || got[0].Recommended != tc.recommended {
				t.Errorf("options %q recommended %d, want %q %d", got[0].Options, got[0].Recommended, tc.options, tc.recommended)
			}
		})
	}
}

// F-0517 crit. 2
func TestParseManyRecommended(t *testing.T) {
	got := Parse("[CLARIFY: q? | A (recommended) | B (recommended) | C]")
	if len(got) != 1 || got[0].Recommended != 1 || !got[0].ManyRecommended {
		t.Fatalf("Parse = %+v, want the first recommended and ManyRecommended", got)
	}
	if !slices.Equal(got[0].Options, []string{"A", "B", "C"}) {
		t.Errorf("options = %q, want every suffix stripped", got[0].Options)
	}
}
