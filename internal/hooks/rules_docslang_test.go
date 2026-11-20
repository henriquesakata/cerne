package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// ptOnly lists every pt-BR word the session text of an English project must
// not carry in what it teaches about documents: the section names that differ
// between the languages, and the planning word (F-0475 crit. 4).
func ptOnly() []string {
	words := []string{doclang.Examples(doclang.PT).Planning}
	for _, k := range []doclang.Kind{doclang.Spec, doclang.Plan, doclang.Result, doclang.Decision, doclang.Runbook} {
		pt, en := doclang.Sections(k, doclang.PT), doclang.Sections(k, doclang.EN)
		for i := range pt {
			if pt[i] != en[i] {
				words = append(words, pt[i])
			}
		}
	}
	return append(words, doclang.Display(doclang.BarLabel, doclang.PT)+":", doclang.AmbiguityMarker(doclang.PT))
}

// line returns the first line of s that has marker.
func line(s, marker string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, marker) {
			return l
		}
	}
	return ""
}

// TestSessionRules_PlanningExampleFollowsTheDocsLanguage (F-0475 crit. 3, 4):
// the planning example and the DOCS LANGUAGE line speak the project's language.
func TestSessionRules_PlanningExampleFollowsTheDocsLanguage(t *testing.T) {
	for _, tt := range []struct {
		lang    doclang.Lang
		planned string
	}{{doclang.EN, "— planning\""}, {doclang.PT, "— planejamento\""}} {
		s := SessionRules(true, tt.lang)
		if !strings.Contains(line(s, "ALREADY-APPROVED"), tt.planned) {
			t.Errorf("%s: planning example lacks %q:\n%s", tt.lang, tt.planned, line(s, "ALREADY-APPROVED"))
		}
		dl := line(s, "DOCS LANGUAGE")
		if dl == "" {
			t.Fatalf("%s: no DOCS LANGUAGE line:\n%s", tt.lang, s)
		}
		for _, want := range append(doclang.Sections(doclang.Spec, tt.lang),
			string(tt.lang), doclang.Display(doclang.BarLabel, tt.lang)+":", doclang.AmbiguityMarker(tt.lang)) {
			if !strings.Contains(dl, want) {
				t.Errorf("%s: DOCS LANGUAGE line lacks %q:\n%s", tt.lang, want, dl)
			}
		}
	}
	en := SessionRules(true, doclang.EN)
	for _, w := range ptOnly() {
		for _, l := range []string{line(en, "ALREADY-APPROVED"), line(en, "DOCS LANGUAGE")} {
			if strings.Contains(l, w) {
				t.Errorf("en: %q in a line an English project reads:\n%s", w, l)
			}
		}
	}
}

// TestSessionRules_NoLanguageKeepsTheNeutralText: an unresolved project gets
// the neutral planning example and no DOCS LANGUAGE line (fail-open).
func TestSessionRules_NoLanguageKeepsTheNeutralText(t *testing.T) {
	s := SessionRules(true, "")
	if strings.Contains(s, "DOCS LANGUAGE") || strings.Contains(line(s, "ALREADY-APPROVED"), doclang.Examples(doclang.PT).Planning) {
		t.Errorf("unresolved language: want the neutral text:\n%s", s)
	}
}

// TestStaticRules_PlanningExampleIsNeutral (F-0475 crit. 4): the texts that
// cannot know the project carry no language word in the planning example.
func TestStaticRules_PlanningExampleIsNeutral(t *testing.T) {
	script, err := os.ReadFile("assets/cerne-register-reminder.sh")
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]string{"RegisterRules": RegisterRules, "script": string(script)} {
		if strings.Contains(s, doclang.Examples(doclang.PT).Planning) {
			t.Errorf("%s still carries the pt-BR planning word", name)
		}
	}
}

// TestLangRules_IsTheTwoLines: what `cerne hook lang-rules` prints is the
// planning item and the DOCS LANGUAGE line of the same language.
func TestLangRules_IsTheTwoLines(t *testing.T) {
	got := LangRules(doclang.EN)
	if !strings.Contains(got, "— planning\"") || !strings.Contains(got, "DOCS LANGUAGE") || strings.Count(got, "\n") != 1 {
		t.Errorf("LangRules(en):\n%s", got)
	}
	if neutral := LangRules(""); strings.Contains(neutral, "DOCS LANGUAGE") || !strings.Contains(neutral, "ALREADY-APPROVED") {
		t.Errorf("LangRules(\"\"):\n%s", neutral)
	}
}

// TestReminder_TakesThePlanningItemFromCerne (F-0475): the Claude Code script
// prints what `cerne hook lang-rules` says instead of its neutral item.
func TestReminder_TakesThePlanningItemFromCerne(t *testing.T) {
	dir := t.TempDir()
	stub := "#!/bin/sh\nif [ \"$2\" = lang-rules ]; then echo 'LANG-RULES-LINE'; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "cerne"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	out := runReminder(t, dir)
	if !strings.Contains(out, "LANG-RULES-LINE") || strings.Contains(out, "<planning, in the docs language>") {
		t.Errorf("script output:\n%s", out)
	}
}
