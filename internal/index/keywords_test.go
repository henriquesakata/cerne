package index

import "testing"

// F-0343 criterion 7: Keywords is the old ask.extractKeywords; F-0367 adds the
// stem prefix of 5+ letter terms.
func TestKeywords(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Por que abandonamos o módulo de pagamentos?", `"abandonamos" OR "abandonam"* OR "módulo" OR "modul"* OR "pagamentos" OR "pagament"*`},
		{"troca de conta chave troca", `"troca" OR "troc"* OR "conta" OR "cont"* OR "chave"`},
		{"Como o é de", "Como o é de"}, // nothing survives: original query back
	}
	for _, c := range cases {
		if got := Keywords(c.in); got != c.want {
			t.Errorf("Keywords(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
