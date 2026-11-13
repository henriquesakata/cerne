package doccheck

import "testing"

// TestSpecLevel (F-0390): the level line in both languages, bold or plain,
// normalized to leve|padrão|avançado; a missing, template or unknown value
// is no level.
func TestSpecLevel(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		ok               bool
	}{
		{"pt bold", "**Tipo:** Melhoria\n**Nível:** padrão — avisos no check\n", "padrão", true},
		{"pt plain", "Nível: leve\n", "leve", true},
		{"pt advanced", "**Nível:** avançado. Design novo\n", "avançado", true},
		{"pt decomposed accents", "**Nível:** padrão\n", "padrão", true},
		{"pt unaccented", "Nivel: avancado\n", "avançado", true},
		{"pt label, en value", "**Nível:** standard\n", "padrão", true},
		{"en bold", "**Level:** light — one package\n", "leve", true},
		{"en plain", "Level: advanced\n", "avançado", true},
		{"en standard", "- **Level:** standard\n", "padrão", true},
		{"emphasized value", "**Nível:** **leve**\n", "leve", true},
		{"first line decides", "**Nível:** leve\n\nNível: avançado\n", "leve", true},
		{"absent", "**Tipo:** Melhoria\n\n## Problema\n", "", false},
		{"template slot", "Nível: leve | padrão | avançado\n", "", false},
		{"template comment", "<!-- METADADOS\nNível: leve | padrão | avançado — capacidade\n-->\n", "", false},
		{"compound", "**Nível:** leve-padrão\n", "", false},
		{"unknown word", "**Nível:** simples\n", "", false},
		{"inside code", "```\nLevel: light\n```\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SpecLevel([]byte(tc.body))
			if got != tc.want || ok != tc.ok {
				t.Fatalf("SpecLevel = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestBarNotApplicable (F-0390 crit. 3): the justification of a "não
// aplicável" bar, in either language; a named or waived bar, or none, is not.
func TestBarNotApplicable(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		ok               bool
	}{
		{"pt", "## Verificação\n\nBarra: não aplicável (efeito local, coberto por teste) — padrão alternativo: TestX\n", "efeito local, coberto por teste", true},
		{"en", "## Verification\n\nBar: not applicable (a regression test covers it) — alternative standard: TestY\n", "a regression test covers it", true},
		{"emphasized", "**Barra:** **não aplicável** (só testes)\n", "só testes", true},
		{"named", "Barra: o jeito de hoje — docs/x.md\n", "", false},
		{"waived", "Barra: dispensada por decisão humana (\"pode pular\")\n", "", false},
		{"none", "## Verificação\n\nTestes.\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := BarNotApplicable([]byte(tc.body))
			if got != tc.want || ok != tc.ok {
				t.Fatalf("BarNotApplicable = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
