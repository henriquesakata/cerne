package dupmatch

import "testing"

// TestSimilarTitles pins the deterministic matcher (F-0147 crit. 3):
// normalized (case/diacritics) token overlap against min(len) with exact
// titles always matching; single-token titles only match exactly.
func TestSimilarTitles(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "Corrigir bug do lock", "Corrigir bug do lock", true},
		{"case+diacritics", "CORREÇÃO do lock", "correcao do lock", true},
		{"shorter title contained", "Guarda de duplicidade", "Guarda de duplicidade no registro: feature new recusa", true},
		{"same-shape different bug", "Corrigir bug do lock", "Corrigir bug do push", false},
		{"unrelated", "Tema dark no grafo", "Endpoint de versão", false},
		{"different feature ids", "Lint de F-0101 quebra no push", "Lint de F-0110 quebra no push", false},
		{"different room ids", "Ata — RM-0003: Revisão da sprint", "Ata — RM-0004: Revisão da sprint", false},
		{"same id case-insensitive", "Lint de f-0101 quebra no push", "Lint de F-0101 quebra no push", true},
		{"id on one side only still overlaps", "Lint de F-0101 quebra no push", "Lint quebra no push", true},
		{"identical long titles", "Desligar capacidade rebaixa a cobrança: informativo sim, fila do agente não", "Desligar capacidade rebaixa a cobrança: informativo sim, fila do agente não", true},
		{"single token needs exact", "lock", "lock do push", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SimilarTitles(c.a, c.b); got != c.want {
				t.Errorf("SimilarTitles(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	same := [][2]string{
		{"https://Example.COM/a/b/", "https://example.com/a/b"},
		{"https://example.com/a#frag", "https://example.com/a"},
		{"HTTP://x.org", "http://x.org/"},
	}
	for _, c := range same {
		if NormalizeURL(c[0]) != NormalizeURL(c[1]) {
			t.Errorf("NormalizeURL(%q) != NormalizeURL(%q)", c[0], c[1])
		}
	}
	if NormalizeURL("https://a.com/x?p=1") == NormalizeURL("https://a.com/x?p=2") {
		t.Error("different queries must stay different")
	}
	if NormalizeURL("https://a.com/x") == NormalizeURL("https://a.com/y") {
		t.Error("different paths must stay different")
	}
}
