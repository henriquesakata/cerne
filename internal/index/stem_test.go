package index

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/slug"
)

// F-0367 criterion 4: the stem of a 5+ letter term, used as a prefix, matches
// the inflected forms the spec lists, in both directions it lists them.
func TestStemMatchesInflections(t *testing.T) {
	pairs := [][2]string{
		{"criptografia", "criptografada"},
		{"troca", "trocar"},
		{"sessão", "sessões"},
		{"sessões", "sessão"},
		{"rotation", "rotating"},
		{"rotating", "rotation"},
	}
	for _, p := range pairs {
		s := stem(slug.Fold(p[0]))
		if !strings.HasPrefix(slug.Fold(p[1]), s) {
			t.Errorf("stem(%q) = %q, does not prefix %q", p[0], s, p[1])
		}
	}
}

func TestStemLeavesShortTermsAndKeepsFourLetters(t *testing.T) {
	for _, short := range []string{"dual", "nota", "casa", "voz"} {
		if got := stem(short); got != short {
			t.Errorf("stem(%q) = %q, want it unchanged (under 5 letters)", short, got)
		}
	}
	for _, term := range []string{"casas", "vozes", "dados", "chave", "troca", "sessoes", "mente"} {
		if got := stem(term); len(got) < 4 {
			t.Errorf("stem(%q) = %q, shorter than 4 letters", term, got)
		}
	}
}

func TestKeywordsAddsStemPrefix(t *testing.T) {
	got := Keywords("troca de conta chave criptografia dual")
	want := "troca OR troc* OR conta OR cont* OR chave OR criptografia OR criptograf* OR dual"
	if got != want {
		t.Fatalf("Keywords = %q, want %q", got, want)
	}
}
