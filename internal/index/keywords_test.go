package index

import "testing"

// F-0343 criterion 7: Keywords is the old ask.extractKeywords, moved unchanged.
func TestKeywords(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Por que abandonamos o módulo de pagamentos?", "abandonamos OR módulo OR pagamentos"},
		{"troca de conta chave troca", "troca OR conta OR chave"},
		{"Como o é de", "Como o é de"}, // nothing survives: original query back
	}
	for _, c := range cases {
		if got := Keywords(c.in); got != c.want {
			t.Errorf("Keywords(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
