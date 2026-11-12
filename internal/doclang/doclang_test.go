package doclang

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Lang
		err  error
	}{
		{"", PT, nil},
		{"pt-BR", PT, nil},
		{" en ", EN, nil},
		{"fr", "", ErrUnknownLang},
		{"EN", EN, nil},
		{"pt-br", PT, nil},
		{"en-US", "", ErrUnknownLang},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if got != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("Parse(%q) = %q, %v; want %q, %v", tt.in, got, err, tt.want, tt.err)
		}
	}
}

func TestSections_PairedAndDistinct(t *testing.T) {
	for _, k := range []Kind{Spec, Plan, Result, Decision, Runbook} {
		pt, en := Sections(k, PT), Sections(k, EN)
		if len(pt) == 0 || len(pt) != len(en) {
			t.Fatalf("kind %d: %d pt sections, %d en", k, len(pt), len(en))
		}
		seen := map[string]bool{}
		for _, list := range [][]string{pt, en} {
			for _, s := range list {
				if s == "" {
					t.Errorf("kind %d: empty heading", k)
				}
				if seen[s] && s != "Gauntlet" { // the one heading both languages share
					t.Errorf("kind %d: heading %q appears twice", k, s)
				}
				seen[s] = true
			}
		}
	}
}

func TestMissing(t *testing.T) {
	all := func(l Lang) map[string]bool {
		m := map[string]bool{}
		for _, s := range Sections(Decision, l) {
			m[s] = true
		}
		return m
	}
	if got := Missing(Decision, all(EN), PT); got != nil {
		t.Errorf("English document: missing %v, want none", got)
	}
	mixed := map[string]bool{"Contexto": true, "Decision": true, "Consequences": true, "Alternativas consideradas": true}
	if got, want := Missing(Decision, mixed, EN), []string{"Implementation and relations"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mixed document: missing %v, want %v", got, want)
	}
	if got, want := Missing(Decision, mixed, PT), []string{"Implementação e relações"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mixed document in pt: missing %v, want %v", got, want)
	}
}

func TestIsSection(t *testing.T) {
	if !IsSection(Spec, 1, " goal and acceptance criteria ") || !IsSection(Spec, 1, "Objetivo e critérios de aceitação") {
		t.Error("criteria section not recognized in both languages")
	}
	if IsSection(Spec, 1, "Problem") {
		t.Error("another section matched the criteria slot")
	}
}

// A catalog with a hole would silently scaffold or read one language short.
func TestCatalog_Complete(t *testing.T) {
	for _, k := range []Kind{Spec, Plan, Result, Decision, Runbook} {
		want := len(sections[k][PT])
		for _, l := range Langs() {
			if got := len(sections[k][l]); got != want {
				t.Errorf("kind %d: %s lists %d sections, pt-BR %d", k, l, got, want)
			}
		}
	}
	for term := Term(0); term < numTerms; term++ {
		for _, l := range Langs() {
			frag, ok := terms[term][l]
			if !ok || frag == "" {
				t.Errorf("term %d has no %s form", term, l)
				continue
			}
			if _, err := regexp.Compile(frag); err != nil {
				t.Errorf("term %d in %s does not compile: %v", term, l, err)
			}
		}
	}
	for term, byLang := range display {
		for _, l := range Langs() {
			if byLang[l] == "" {
				t.Errorf("term %d has no %s display form", term, l)
			}
		}
	}
	if got := Sections(Spec, PT)[SpecCriteria]; got != "Objetivo e critérios de aceitação" {
		t.Errorf("SpecCriteria points at %q", got)
	}
}

func TestMissing_IgnoresCase(t *testing.T) {
	present := map[string]bool{}
	for _, s := range Sections(Runbook, EN) {
		present[" "+strings.ToUpper(s)+" "] = true
	}
	if got := Missing(Runbook, present, EN); got != nil {
		t.Errorf("upper-cased headings reported missing: %v", got)
	}
}

func TestTerms(t *testing.T) {
	if !Is(Loss, "Derrota") || !Is(Loss, " loss ") || Is(Loss, "lossless") || Is(Loss, "tie") {
		t.Error("Is(Loss) misclassifies")
	}
	if !Is(Win, "vito\u0301ria") { // decomposed accent, as pasted from macOS
		t.Error("Is(Win) refuses the decomposed accent")
	}
	if LangAt(NotApplicable, "Not applicable (x)") != EN || LangAt(NotApplicable, "não aplicável (x)") != PT || LangAt(Waived, "something else") != PT {
		t.Error("LangAt picks the wrong language")
	}
	if Strict()[0] != EN || Langs()[0] != PT {
		t.Error("PT must be the first language and the only loose one")
	}
	if got := Alt(Round); got != "(?:Rodada|Round)" {
		t.Errorf("Alt(Round) = %q", got)
	}
}
