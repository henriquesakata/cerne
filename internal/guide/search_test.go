package guide

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func titles(secs []Section) string {
	var t []string
	for _, s := range secs {
		t = append(t, s.Title)
	}
	return strings.Join(t, "|")
}

func TestSearchFoldsAccentAndCase(t *testing.T) {
	secs := []Section{{Title: "Configuração", Source: "faq", Text: "muda o valor"}, {Title: "Outra", Source: "faq", Text: "nada"}}
	if got := titles(Search(secs, "CONFIGURACAO", 3)); got != "Configuração" {
		t.Fatalf("got %q", got)
	}
}

func TestSearchCoverageBeatsBM25(t *testing.T) {
	secs := []Section{
		{Title: "many", Source: "faq", Text: strings.Repeat("settings ", 30)},
		{Title: "both", Source: "faq", Text: "settings scope"},
	}
	if got := titles(Search(secs, "settings scope", 3)); got != "both|many" {
		t.Fatalf("got %q, want both|many", got)
	}
}

func TestSearchTitleTermsCount(t *testing.T) {
	secs := []Section{
		{Title: "body", Source: "faq", Text: "handoff agent"},
		{Title: "handoff agent", Source: "faq", Text: "x"},
	}
	if got := titles(Search(secs, "handoff agent", 3)); got != "handoff agent|body" {
		t.Fatalf("got %q", got)
	}
}

func TestSearchLimitCutAndEmpty(t *testing.T) {
	long := strings.Repeat("é", 2000) + " widget"
	secs := []Section{{Title: "a", Source: "faq", Text: "widget " + long}, {Title: "b", Source: "faq", Text: "widget"}, {Title: "c", Source: "faq", Text: "widget"}}
	got := Search(secs, "widget", 2)
	if len(got) != 2 {
		t.Fatalf("limit: %d sections", len(got))
	}
	for _, s := range got {
		if n := utf8.RuneCountInString(s.Text); n > 1200 {
			t.Errorf("%s: %d runes", s.Title, n)
		}
	}
	if !strings.HasSuffix(got[0].Text, "…") && !strings.HasSuffix(got[1].Text, "…") {
		t.Errorf("cut text lacks the ellipsis")
	}
	if len(Search(secs, "zzzz", 3)) != 0 || len(Search(secs, "  ", 3)) != 0 {
		t.Errorf("no match / blank query must return nothing")
	}
}

func TestSearchShortQueryFallsBack(t *testing.T) {
	secs := []Section{{Title: "UI", Source: "faq", Text: "api of the ui"}}
	if got := titles(Search(secs, "ui", 3)); got != "UI" {
		t.Fatalf("got %q", got)
	}
}

func TestSearchStopwordOnlyQueryMatchesNothing(t *testing.T) {
	secs := []Section{{Title: "The And", Source: "faq", Text: "the and a b"}}
	for _, q := range []string{"the", "AND", `a "b`, "to the"} {
		if got := Search(secs, q, 3); len(got) != 0 {
			t.Errorf("Search(%q) = %v, want nothing", q, titles(got))
		}
	}
	if got := titles(Search(secs, "ui", 3)); got != "" {
		t.Errorf("ui is not in this corpus: %q", got)
	}
}

func TestSearchTieBreaksFAQThenSkillThenHelp(t *testing.T) {
	secs := []Section{
		{Title: "cerne settings", Source: "help: cerne settings", Text: strings.Repeat("settings ", 20)},
		{Title: "Settings", Source: "skill: Settings", Text: strings.Repeat("settings ", 10)},
		{Title: "How to settings", Source: "faq", Text: "settings"},
	}
	// "settings" covers one term in each; titles hold it in all three.
	if got := titles(Search(secs, "settings", 3)); got != "How to settings|Settings|cerne settings" {
		t.Fatalf("got %q", got)
	}
}

// TestNamedCommandHelpTakesTheLastSlotOnlyBeyondOne: asking for a command by
// name brings its help into the limit (F-0399), but with --limit 1 the single
// best hit is never replaced.
func TestNamedCommandHelpTakesTheLastSlotOnlyBeyondOne(t *testing.T) {
	secs := []Section{
		{Title: "Show everything", Source: "faq", Text: strings.Repeat("show ", 30)},
		{Title: "Showing it", Source: "faq", Text: strings.Repeat("show ", 20)},
		{Title: "Show off", Source: "skill: Show off", Text: strings.Repeat("show ", 10)},
		{Title: "cerne show", Source: "help: cerne show", Text: "show"},
	}
	if got := titles(Search(secs, "show", 1)); strings.Contains(got, "cerne show") {
		t.Errorf("limit 1 replaced the best hit: %q", got)
	}
	if got := titles(Search(secs, "show", 2)); !strings.Contains(got, "cerne show") {
		t.Errorf("limit 2 lost the named command: %q", got)
	}
}
