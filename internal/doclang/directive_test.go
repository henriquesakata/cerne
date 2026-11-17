package doclang

import (
	"regexp"
	"strings"
	"testing"
)

// F-0447 criterion 2: the directive that closes every prompt names the
// language and quotes each protocol form in that language, and the forms it
// quotes are the ones the real readers accept.
func TestDirective_QuotesTheFormsOfItsLanguage(t *testing.T) {
	for _, l := range Langs() {
		d := Directive(l)
		if d == "" {
			t.Fatalf("Directive(%s) is empty", l)
		}
		name := map[Lang]string{PT: "Brazilian Portuguese", EN: "English"}[l]
		if !strings.Contains(d, name) {
			t.Errorf("Directive(%s) does not name the language %q", l, name)
		}
		for _, term := range []Term{BarLabel, MethodLabel, ExitLabel, NotApplicable, Waived, Round, CriticFindings, Win, Loss, Tie, Criterion, Unmet} {
			if w := Display(term, l); w == "" || !strings.Contains(d, w) {
				t.Errorf("Directive(%s) does not quote %q", l, w)
			}
		}
		if w := map[Lang]string{PT: "esforço", EN: "effort"}[l]; !strings.Contains(d, "· "+w+" <level>") {
			t.Errorf("Directive(%s) does not quote the agent line with %q", l, w)
		}
		if !strings.Contains(d, AmbiguityMarker(l)) {
			t.Errorf("Directive(%s) does not quote the ambiguity marker", l)
		}
		other := PT
		if l == PT {
			other = EN
		}
		if strings.Contains(d, AmbiguityMarker(other)) || strings.Contains(d, Display(BarLabel, other)+":") {
			t.Errorf("Directive(%s) quotes a form of %s", l, other)
		}
	}
}

// The forms the directive quotes round-trip through the real readers, in the
// language they were written in.
func TestDirective_FormsRoundTripThroughReaders(t *testing.T) {
	for _, l := range Langs() {
		d := Directive(l)
		bar := Display(BarLabel, l) + ": Kiro spec mode"
		if v, got, ok := BarLine(bar, false); !ok || got != l || v != "Kiro spec mode" {
			t.Errorf("%s: BarLine(%q) = %q %s %v", l, bar, v, got, ok)
		}
		for _, e := range []Term{NotApplicable, Waived} {
			val := Display(e, l) + " (reason)"
			if !strings.Contains(d, Display(e, l)) {
				t.Errorf("%s: the directive lacks %q", l, Display(e, l))
			}
			if tt, got, _, complete, ok := Exemption(val); !ok || !complete || tt != e || got != l {
				t.Errorf("%s: Exemption(%q) = %v %s %v %v", l, val, tt, got, complete, ok)
			}
		}
		round := Display(Round, l) + " 2 — " + Display(Win, l)
		if n, got, ok := RoundAnchor(round); !ok || n != 2 || got != l {
			t.Errorf("%s: RoundAnchor(%q) = %d %s %v", l, round, n, got, ok)
		}
		if !CriticLine(Display(CriticFindings, l) + ": none") {
			t.Errorf("%s: CriticLine does not read %q", l, Display(CriticFindings, l))
		}
		if !HeaderLine(Display(MethodLabel, l)+": blind") || !HeaderLine(Display(ExitLabel, l)+": win or tie") {
			t.Errorf("%s: HeaderLine does not read the method/exit labels", l)
		}
		unmet := "- " + Display(Criterion, l) + " 2: " + Display(Unmet, l)
		if l == EN && !UnmetCriterion(unmet) {
			t.Errorf("%s: UnmetCriterion does not read %q", l, unmet)
		}
		if cs := Citations(Display(Criterion, l) + " 2"); len(cs) != 1 {
			t.Errorf("%s: Citations(%q) = %v", l, Display(Criterion, l)+" 2", cs)
		}
	}
}

// The directive closes the prompt: it is the last thing the model reads.
func TestDirective_StartsWithLanguageLine(t *testing.T) {
	for _, l := range Langs() {
		if d := Directive(l); !strings.HasPrefix(d, "\n\nDOCUMENT LANGUAGE") {
			t.Errorf("Directive(%s) = %.40q", l, d)
		}
	}
}

// Reply closes a prompt whose output is a short answer, not a document: it
// names the language and nothing else.
func TestReply_NamesTheLanguage(t *testing.T) {
	for l, name := range map[Lang]string{PT: "Brazilian Portuguese", EN: "English"} {
		r := Reply(l)
		if !strings.HasPrefix(r, "\n\nLANGUAGE") || !strings.Contains(r, name) {
			t.Errorf("Reply(%s) = %q", l, r)
		}
		if strings.Contains(r, "SYSTEM SHALL") || strings.Contains(r, AmbiguityMarker(l)) {
			t.Errorf("Reply(%s) carries document forms: %q", l, r)
		}
	}
}

// The alternative stop standard that follows a "not applicable" bar is a form
// of the document too: the contract quotes it in the project's language.
func TestDisplay_AlternativeStandard(t *testing.T) {
	for l, want := range map[Lang]string{PT: "padrão alternativo", EN: "alternative standard"} {
		if got := Display(AlternativeStandard, l); got != want {
			t.Errorf("Display(AlternativeStandard, %s) = %q want %q", l, got, want)
		}
		if !Is(AlternativeStandard, want) {
			t.Errorf("Is(AlternativeStandard, %q) = false", want)
		}
	}
}

// The forms of the Backlog chat are quoted from the catalog in both languages.
func TestDisplay_ChatForms(t *testing.T) {
	for _, c := range []struct {
		t      Term
		pt, en string
	}{
		{Recommended, "recomendada", "recommended"},
		{OriginAccepted, "recomendação aceita", "recommendation accepted"},
		{OriginOther, "outra opção", "other option"},
		{OriginFree, "resposta livre do usuário", "user's free answer"},
		{Assumed, "Assumi:", "Assumed:"},
		{TreeProgress, "Árvore: <n> resolvidas · <n> abertas · <n> suposições", "Tree: <n> resolved · <n> open · <n> assumptions"},
		{ProposeNow, "propõe já", "propose now"},
	} {
		if got := Display(c.t, PT); got != c.pt {
			t.Errorf("Display(%d, PT) = %q want %q", c.t, got, c.pt)
		}
		if got := Display(c.t, EN); got != c.en {
			t.Errorf("Display(%d, EN) = %q want %q", c.t, got, c.en)
		}
	}
}

// F-0447: the free text a prompt has the model copy is in the language of the
// project: no English word inside the pt-BR examples, and every field set.
func TestExamples_AreInTheirOwnLanguage(t *testing.T) {
	english := regexp.MustCompile(`(?i)\b(the|of|by|and|or|with|win|will|should|see|report|fails|use|tie|loss|same|class|guide|human|comparison|critic|fresh|context|covered|uncovered|task|relevant|findings|where|adjust|sources|parallel|high|medium|low|test|until|opens|stopping|only|recorded)\b`)
	for _, l := range Langs() {
		p := Examples(l)
		fields := map[string]string{
			"Method": p.Method, "Exit": p.Exit, "BarExample": p.BarExample, "Alternative": p.Alternative,
			"Parallel": p.Parallel, "Severities": p.Severities, "NoFindings": p.NoFindings,
			"Covered": p.Covered, "Uncovered": p.Uncovered, "LevelLine": p.LevelLine, "OriginExample": p.OriginExample, "DecisionVoice": p.DecisionVoice, "DecisionTitle": p.DecisionTitle, "ExpectedOutput": p.ExpectedOutput, "Consulted": p.Consulted, "WhereIs": p.WhereIs, "HowToAdjust": p.HowToAdjust, "SourcesRead": p.SourcesRead,
		}
		for name, v := range fields {
			if v == "" {
				t.Errorf("%s: %s is empty", l, name)
			}
			if l == PT && english.MatchString(strings.ReplaceAll(v, "Kiro", "")) {
				t.Errorf("pt: %s %q carries an English word %q", name, v, english.FindString(v))
			}
		}
	}
}
