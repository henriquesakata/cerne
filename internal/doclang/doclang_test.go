package doclang

import (
	"errors"
	"reflect"
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
		{"EN", "", ErrUnknownLang},
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
