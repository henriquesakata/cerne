package lint

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestResultGauntletFindings(t *testing.T) {
	after := resultGauntletSince.Add(24 * time.Hour)
	before := resultGauntletSince.Add(-24 * time.Hour)

	recorded := "## Resultado\n\nfeito\n\n## Gauntlet\n\nBarra: middleware do finance — apps/finance/web/middleware.ts\nRodada 1 — **vitória**: \"Só A tira do matcher as rotas públicas\"\nAchados do crítico: 2 — corrigidos 1; refutados 1 com file:line\n\n## Débitos\n\nnada\n"
	exempt := "## Resultado\n\nfeito\n\n## Gauntlet\n\nBarra: dispensada por decisão humana (\"segue sem barra dessa vez\")\n"
	emptySection := "## Resultado\n\nfeito\n\n## Gauntlet\n\nrodamos o loop, confia\n"

	cases := []struct {
		name        string
		state       domain.FeatureState
		completedAt time.Time
		result      string
		wantDetail  string // "" = no finding
	}{
		{"verdict recorded is clean", domain.StateImplemented, after, recorded, ""},
		{"exemption recorded is clean", domain.StateImplemented, after, exempt, ""},
		{"nfd verdict is clean", domain.StateDone, after, "## Gauntlet\nBarra: x\nRodada 1 \u2014 vito\u0301ria: \u201ccita\u00e7a\u0303o\u201d\n", ""},
		{"quote on another line does not vouch", domain.StateImplemented, after, "## Gauntlet\nBarra: \"refer\u00eancia\"\nRodada 1 \u2014 vit\u00f3ria\n", "\"Rodada 1\" has no judge quote"},
		{"no section flags", domain.StateImplemented, after, "## Resultado\n\nfeito\n", `no "## Gauntlet" section`},
		{"section without cited verdict flags", domain.StateImplemented, after, emptySection, "no \"Barra:\" line"},
		{"verdict without quote flags", domain.StateImplemented, after, "## Gauntlet\nBarra: x\nRodada 1 — vitória\n", "\"Rodada 1\" has no judge quote"},
		{"completed before cutoff is grandfathered", domain.StateImplemented, before, "## Resultado\n\nfeito\n", ""},
		{"live states are ignored", domain.StateInDevelopment, after, "## Resultado\n\nfeito\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if err := store.SaveFeature(dir, f); err != nil {
				t.Fatal(err)
			}
			writeDoc(t, dir, "content/features/F-0001/result.md", tc.result)
			events := []domain.Event{{EntityID: "F-0001", Type: "feature.completed", At: tc.completedAt}}

			got := resultGauntletFindings(dir, []store.Feature{f}, events)
			if tc.wantDetail == "" {
				if len(got) != 0 {
					t.Fatalf("want no findings, got %v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want 1 finding, got %v", got)
			}
			if got[0].Rule != RuleResultMissingGauntlet || got[0].Entity != "F-0001" {
				t.Fatalf("finding = %+v", got[0])
			}
			if !strings.Contains(got[0].Detail, tc.wantDetail) {
				t.Fatalf("detail %q lacks %q", got[0].Detail, tc.wantDetail)
			}
		})
	}
}

// A result.md that never materialized counts as zero record, same stance as
// result-missing-evidence: doc absence is missing-gate-doc's finding, but the
// loop still has no proof.
func TestResultGauntletMissingDoc(t *testing.T) {
	dir := t.TempDir()
	f := store.Feature{ID: "F-0001", Title: "t", State: domain.StateImplemented}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatal(err)
	}
	events := []domain.Event{{EntityID: "F-0001", Type: "feature.completed", At: resultGauntletSince.Add(time.Hour)}}
	got := resultGauntletFindings(dir, []store.Feature{f}, events)
	if len(got) != 1 || !strings.Contains(got[0].Detail, `no "## Gauntlet" section`) {
		t.Fatalf("want one missing-section finding, got %v", got)
	}
}

// gauntletProblem runs the round reader on a section body.
func gauntletProblem(section string) string { return parseGauntlet(section).problem(doclang.PT) }

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	secs := gauntletSections(b)
	if len(secs) == 0 {
		t.Fatalf("%s has no Gauntlet section", name)
	}
	return secs[0]
}

type gauntletCase struct {
	name    string
	section string
	want    string // "" = clean, else substring of problem()
}

func runGauntletCases(t *testing.T, cases []gauntletCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gauntletProblem(tc.section)
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("problem() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Criterion 1: heading round + judge quote in the paragraph below (T-0032).
func TestGauntletHeadingRounds(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"t0032 original fixture", readFixture(t, "gauntlet-t0032-original.md"), ""},
		{"heading, quote below, literal verdict", "Barra: x\n### Rodada 1\nJuiz: \"cita\" \u2014 VEREDITO: B.\n", ""},
		{"fechamento heading stays inside last round", "Barra: x\n### Rodada 1 \u2014 vit\u00f3ria\n### Fechamento\nJuiz: \"cita\"\n", ""},
		{"rodada mention mid-sentence opens no block", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"cita\"\nver a Rodada 2 depois\n", ""},
	})
}

// Criterion 2: list, bold, plain line and table row (T-0033 + skill template).
func TestGauntletRoundLayouts(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"t0033 fixture", readFixture(t, "gauntlet-t0033.md"), ""},
		{"bold list item", "- **Barra:** x\n- **Rodada 1 \u2014 vit\u00f3ria.** Juiz: \"cita\"\n", ""},
		{"plain line", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"cita\"\n", ""},
		{"numbered list", "Barra: x\n1. Rodada 1 \u2014 empate: \u201ccita\u201d\n", ""},
		{"table row", "Barra: x\n| Rodada 1 | vit\u00f3ria | \"x\" |\n", ""},
		{"blockquote", "> Barra: x\n> Rodada 1 \u2014 vit\u00f3ria: \u00abcita\u00bb\n", ""},
		{"skill template", "Barra: x\nMétodo: y\nRodada 1 \u2014 vit\u00f3ria: \"cita\"\nAchados do crítico: 2 \u2014 corrigidos 1; refutados 1 com file:line\n", ""},
		{"no anchor: implicit round", "- **Barra:** x\n- **vit\u00f3ria** \u2014 juiz: \"cita\"\n", ""},
		{"two rounds, both complete", "Barra: x\nRodada 1 \u2014 derrota: \"a\"\nRodada 2 \u2014 vit\u00f3ria: \"b\"\n", ""},
	})
}

// Criterion 3.
func TestGauntletRoundWithoutQuote(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"quote only on the Barra line", "Barra: \"ref\"\nRodada 1 \u2014 vit\u00f3ria\n", "\"Rodada 1\" has no judge quote"},
		{"quote only in round 1", "Barra: x\nRodada 1 \u2014 derrota: \"a\"\nRodada 2 \u2014 vit\u00f3ria\n", "\"Rodada 2\" has no judge quote"},
		{"quote only in Achados (NFC)", "Barra: x\nRodada 1 \u2014 vit\u00f3ria\nAchados do cr\u00edtico: 1 \u2014 \"x\"\n", "\"Rodada 1\" has no judge quote"},
		{"quote only in Achados (NFD)", "Barra: x\nRodada 1 \u2014 vit\u00f3ria\nAchados do cri\u0301tico: 1 \u2014 \"x\"\n", "\"Rodada 1\" has no judge quote"},
		{"empty quote pair", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"  \"\n", "\"Rodada 1\" has no judge quote"},
		{"lone quote char", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: 5\" tela\n", "\"Rodada 1\" has no judge quote"},
		{"table: citation only in the next row", "Barra: x\n| Rodada 1 | vit\u00f3ria |\n| Rodada 2 | vit\u00f3ria | \"y\" |\n", "\"Rodada 1\" has no judge quote"},
		{"only Barra, no round", "Barra: x\n", "the round has no judge quote"},
		{"implicit round: quote only on Barra", "Barra: \"x\"\nvit\u00f3ria sem citar\n", "the round has no judge quote"},
	})
}

// Criterion 4.
func TestGauntletRoundWithoutVerdict(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"quote, no verdict", "Barra: x\nRodada 1: \"cita\"\n", "\"Rodada 1\" has no verdict"},
		{"verdict only in other round", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\nRodada 2: \"b\"\n", "\"Rodada 2\" has no verdict"},
		{"literal EMPATE + quote", "Barra: x\nRodada 1: \"cita\"\nVEREDITO: EMPATE\n", ""},
		{"literal A, lowercase keyword", "Barra: x\nRodada 1: \"cita\"\nveredito: A\n", ""},
		{"VEREDITO: C is no verdict", "Barra: x\nRodada 1: \"cita\"\nVEREDITO: C\n", "\"Rodada 1\" has no verdict"},
	})
}

// Criterion 5.
func TestGauntletMissingBar(t *testing.T) {
	complete := "Rodada 1 \u2014 vit\u00f3ria: \"cita\"\n"
	runGauntletCases(t, []gauntletCase{
		{"no Barra line", complete, `no "Barra:" line`},
		{"empty Barra", "Barra:\n" + complete, `no "Barra:" line`},
		{"bold Barra without value", "**Barra:**\n" + complete, `no "Barra:" line`},
		{"bold list Barra with value", "- **Barra:** x\n" + complete, ""},
		{"quoted Barra", "> Barra: x\n" + complete, ""},
		{"Barra mid-sentence is no Barra line", "ver Barra: x\n" + complete, `no "Barra:" line`},
	})
}

// Criterion 6.
func TestGauntletExemptions(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"n\u00e3o aplic\u00e1vel NFC", "Barra: n\u00e3o aplic\u00e1vel (corretivo de uma linha)\n", ""},
		{"n\u00e3o aplic\u00e1vel NFD", "Barra: na\u0303o aplica\u0301vel (x)\n", ""},
		{"dispensada NFC", "Barra: dispensada por decis\u00e3o humana (\"segue\")\n", ""},
		{"dispensada NFD", "Barra: dispensada por decisa\u0303o humana (x)\n", ""},
		{"bold list exemption", "- **Barra:** n\u00e3o aplic\u00e1vel (x)\n", ""},
		{"exemption with rounds incomplete", "Barra: dispensada por decis\u00e3o humana (x)\nRodada 1 \u2014 vit\u00f3ria\n", ""},
		{"exemption without parenthesis is no exemption", "Barra: n\u00e3o aplic\u00e1vel\n", "the round has no judge quote"},
	})
}

// Criterion 8.
func TestGauntletProblemOrder(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"no bar and incomplete round: bar only", "Rodada 1 \u2014 vit\u00f3ria\n", `no "Barra:" line`},
		{"quote before verdict", "Barra: x\nRodada 1\n", "\"Rodada 1\" has no judge quote"},
		{"first incomplete round named", "Barra: x\nRodada 1: \"a\" VEREDITO: A\nRodada 2: \"b\"\nRodada 3\n", "\"Rodada 2\" has no verdict"},
	})
	if got := gauntletProblem("Rodada 1 \u2014 vit\u00f3ria\n"); strings.Contains(got, "rodada") {
		t.Fatalf("bar problem must not mention rounds: %q", got)
	}
}

// Same round number = one round; the anchor is the capitalised literal.
func TestGauntletMergedRounds(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"later heading of the same round", "Barra: x\nRodada 2 \u2014 vit\u00f3ria: \"x\"\n### Rodada 2 \u2014 correções\ncommits abc\n", ""},
		{"builder line + judge line", "Barra: x\nRodada 2 (15:50) \u2014 fix builder de contexto novo\nRodada 2 \u2014 juiz cego \u2014 vit\u00f3ria: \"x\"\n", ""},
		{"disposition after Achados merges", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\nRodada 2 \u2014 vit\u00f3ria: \"b\"\nAchados do crítico: 2\n- Rodada 2 \u2192 `.cernerc` ileg\u00edvel\n", ""},
		{"lowercase wrapped prose opens no round", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\n  rodada 2 aberta com crítico novo.\n", ""},
		{"uppercase RODADA is no anchor", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\nRODADA 2 sem nada\n", ""},
		{"incomplete numbered round still flags", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\nRodada 2 \u2014 correções\n### Rodada 2 \u2014 notas\n", "\"Rodada 2\" has no judge quote"},
		{"merged round, quote but no verdict", "Barra: x\nRodada 2: \"q\"\nRodada 2 \u2014 notas\n", "\"Rodada 2\" has no verdict"},
	})
}

// A short qualifier between "Barra" and the colon still makes a bar line.
func TestGauntletBarQualifier(t *testing.T) {
	round := "Rodada 1 \u2014 vit\u00f3ria: \"c\"\n"
	long := "Barra " + strings.Repeat("x", 41) + ": v\n"
	runGauntletCases(t, []gauntletCase{
		{"da spec", "Barra da spec: x\n" + round, ""},
		{"herdada da spec", "Barra herdada da spec: x\n" + round, ""},
		{"parenthesised", "Barra (herdada da spec): x\n" + round, ""},
		{"bold parenthesised", "**Barra (herdada da spec):** x\n" + round, ""},
		{"list with qualifier", "- Barra (frente B): x\n" + round, ""},
		{"lowercase qualified", "barra da spec: x\n" + round, ""},
		{"qualified exemption", "Barra da spec: n\u00e3o aplic\u00e1vel (y)\n", ""},
		{"bold qualified exemption NFD", "**Barra (da spec):** na\u0303o aplica\u0301vel (y)\n", ""},
		{"mid-line after round anchor", "**Rodada 1 \u2014 vit\u00f3ria.** Barra: x \"c\"\n", `no "Barra:" line`},
		{"prose exemption mid-line", "A spec fixou Barra: n\u00e3o aplic\u00e1vel (y)\n" + round, `no "Barra:" line`},
		{"veredito barra prose", "Veredito: barra n\u00e3o aplic\u00e1vel (y)\n" + round, `no "Barra:" line`},
		{"qualifier over 40 chars", long + round, `no "Barra:" line`},
		{"no colon", "Barra da spec x\n" + round, `no "Barra:" line`},
		{"empty value", "Barra da spec:\n" + round, `no "Barra:" line`},
		{"empty bold value", "**Barra (da spec):**\n" + round, `no "Barra:" line`},
		{"word glued to Barra is no qualifier", "Barras: x\n" + round, `no "Barra:" line`},
	})
}

// C1: emphasis may sit between "Barra" and its colon.
func TestGauntletBarEmphasis(t *testing.T) {
	round := "Rodada 1 — vitória: \"c\"\n"
	runGauntletCases(t, []gauntletCase{
		{"colon outside bold in list", "- **Barra**: x\n" + round, ""},
		{"bold then qualifier", "**Barra** (da spec): x\n" + round, ""},
		{"underscore emphasis", "_Barra_: x\n" + round, ""},
		{"emphasised exemption value", "**Barra:** *não aplicável* (corretivo)\n", ""},
		{"exemption after colon-outside bold", "**Barra**: _não aplicável_ (x)\n", ""},
	})
}

// C2+C3: "Achados do crítico" opens a critic block, not a round cut.
func TestGauntletCriticBlock(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"A: anchor inside critic mode reopens nothing", "Barra: x\nRodada 2 — vitória\nAchados do crítico: 1\n- Rodada 2 → \"lock stale…\"\n", "\"Rodada 2\" has no judge quote"},
		{"B: anchor in critic mode does not anchor", "Barra: x\n- **vitória** — juiz: \"x\"\nAchados do crítico: 1\n- Rodada 1 → refutado\n", ""},
		{"singular Achado", "Barra: x\nRodada 1 — vitória\nAchado do crítico: \"x\"\n", "\"Rodada 1\" has no judge quote"},
		{"NFD critico", "Barra: x\nRodada 1 — vitória\nAchados do cri\u0301tico: \"x\" empate\n", "\"Rodada 1\" has no judge quote"},
		{"C3: judge paragraph after the Achados line is critic text", "Barra: x\n### Rodada 1\n**Achados do crítico:** 2 — corrigidos\n\n**Juiz** (A = nosso): \"hierarquia clara\" — VEREDITO: A\n", "\"Rodada 1\" has no judge quote"},
		{"critic ends at heading", "Barra: x\nRodada 1 — vitória\nAchados do crítico: 1\n### Fechamento\nJuiz: \"c\"\n", ""},
		{"list item after blank stays critic", "Barra: x\nRodada 1 — vitória\nAchados do crítico: 1\n\n- item \"q\"\n", "\"Rodada 1\" has no judge quote"},
		{"critic verdict word does not count", "Barra: x\nRodada 1: \"c\"\nAchados do crítico: 1 — empate\n", "\"Rodada 1\" has no verdict"},
		{"dense layout: non-list anchor ends critic mode", "Barra: x\nRodada 1 — vitória: \"a\"\nAchados do crítico: 1 — corrigido\nRodada 2 — crítico, sem juiz\n", "\"Rodada 2\" has no judge quote"},
		{"T-0040 shape", "Barra: x\nRodada 1 — derrota: \"VEREDITO: B\"\nAchados do crítico: 7 — \"VEREDITO DO CRÍTICO: 1 HIGH\"\nRodada 2 — derrota: \"VEREDITO: A\"\nAchados do crítico: 8 — corrigidos 7\nRodada 4 do crítico, sobre as correções (sem juiz): \"VEREDITO DO CRÍTICO: 0 HIGH\"\nAchados do crítico: 6 — corrigidos 4\nEncerramento perdendo por decisão humana: \"Encerrar\"\n", "\"Rodada 4\" has no verdict"},
		{"bold non-list anchor ends critic mode", "Barra: x\nRodada 1 — vitória: \"a\"\nAchados do crítico: 1\n**Rodada 2 — notas**\n", "\"Rodada 2\" has no judge quote"},
		{"F-0275 shape", "Barra: x\nRodada 1 — vitória: \"VEREDITO: A\" — \"a\"\nRodada 2 — vitória: \"VEREDITO: B\" — \"b\"\nAchados do crítico: 8 — corrigidos 5\n```\n\n**Achados do crítico de contexto fresco (rodada 1):**\n\n- **Corrigidos (5):** (1) x\n- **Refutado (1):** (6) \"campo sem enum\"\n\n**Lacunas apontadas pelos juízes:**\n\n- Rodada 2 → `.cernerc` ilegível\n\n**Nota de método:** a rodada 2 apontou\n", ""},
	})
}

// Critic block (reviewer R1-R4, R6): it ends only at an ATX heading, a
// non-list round anchor, or a sibling list-item anchor of a list-item Achados.
func TestGauntletCriticBlockEnds(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"R1: sibling list-item anchor ends a list-item Achados", "Barra: x\n- **Rodada 1 — derrota.** Juiz: \"B\" — VEREDITO: B\n- **Achados do crítico:** 3\n- **Rodada 2 — vitória.**\n", "\"Rodada 2\" has no judge quote"},
		{"nested list-item anchor stays critic", "Barra: x\n- **Rodada 1 — vitória.** \"a\"\n- **Achados do crítico:** 3\n  - Rodada 2 → \"lock stale\"\n", ""},
		{"R2: numbered disposition with quote on continuation", "Barra: x\nRodada 1 — vitória\nAchados do crítico: 2\n1. corrigido (x)\n\n   \"lock stale\" no arquivo\n", "\"Rodada 1\" has no judge quote"},
		{"R3: Achados heading, blank, table with quote", "Barra: x\nRodada 1 — vitória\n### Achados do crítico\n\n| # | \"q\" |\n", "\"Rodada 1\" has no judge quote"},
		{"R4: #1 is not a heading", "Barra: x\nRodada 1 — vitória\nAchados do crítico: 2\n#1 lock stale\n#2 \"x\"\n", "\"Rodada 1\" has no judge quote"},
		{"a real heading still ends it", "Barra: x\nRodada 1 — vitória\nAchados do crítico: 2\n#1 lock stale\n### x\nJuiz: \"c\"\n", ""},
		{"S1: a heading that is also a Saída line ends it", "Barra: x\nRodada 1 — vitória\nAchados do crítico: 1 — corrigido\n### Saída: vencer\nJuiz: \"hierarquia clara\"\n", ""},
		{"S2: sibling item of an existing round does not end it", "Barra: x\n- **Rodada 1 — vitória.** Juiz escolheu A.\n- **Achados do crítico:** 1\n- Rodada 1 → \"lock stale\" corrigido\n", "\"Rodada 1\" has no judge quote"},
		{"R6: Barra right after Achados is the bar", "Rodada 1 — vitória: \"c\"\nAchados do crítico: 1\nBarra: x\n", ""},
	})
}

// R5, R7, R9.
func TestGauntletHeaderEdges(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"E-0003 shape: exemption citing child verdicts", "Barra: n\u00e3o aplic\u00e1vel (x); padr\u00e3o alternativo cumprido: filha \"VEREDITO: B\"\n", ""},
		{"accepted limit (R5): Sa\u00edda carrying the literal is still consumed", "Barra: x\nRodada 1: \"c\"\nSa\u00edda: VEREDITO: A\n", "\"Rodada 1\" has no verdict"},
		{"accepted limit (R5): M\u00e9todo listing the literals is consumed", "Barra: x\nRodada 1: \"c\"\nM\u00e9todo: juiz responde VEREDITO: A, B ou EMPATE\n", "\"Rodada 1\" has no verdict"},
		{"R7: emphasised Método/Saída are consumed", "Barra: x\n**Método**: y \"vitória\"\n**Saída**: derrota\n", "the round has no judge quote"},
		{"R9: NBSP inside the exemption", "Barra: n\u00e3o\u00a0aplic\u00e1vel (x)\n", ""},
		{"S4: NBSP-only bar value is no value", "Barra:\u00a0\nRodada 1 — vitória: \"c\"\n", `no "Barra:" line`},
		{"R9: NBSP after the colon", "Barra:\u00a0n\u00e3o aplic\u00e1vel (x)\n", ""},
	})
}

// C4: every bar line is consumed; any of them may carry the exemption.
func TestGauntletBarLinesConsumed(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"second bar line inside round 2 is not a quote", "Barra: x\nRodada 1 — vitória: \"a\"\nRodada 2 — vitória\nBarra (herdada da spec): y (\"x\")\n", "\"Rodada 2\" has no judge quote"},
		{"exemption on the second bar line", "Barra da spec: x\nBarra: dispensada por decisão humana (y)\n", ""},
	})
}

// C5: keyword case-insensitive, value case-sensitive.
func TestGauntletVerdictLiteralCase(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"a definir is no verdict", "Barra: x\nRodada 1: \"c\"\nVeredito: a definir\n", "\"Rodada 1\" has no verdict"},
		{"Veredito: B", "Barra: x\nRodada 1: \"c\"\nVeredito: B\n", ""},
	})
}

// C6: Método/Saída header lines are consumed; the implicit round reads the whole block.
func TestGauntletHeaderLinesAndImplicitRound(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"header lines do not feed the implicit round", "Barra: x\nMétodo: y \"vitória\"\nSaída: vencer; derrota abre nova rodada\n\nO crítico disse \"foi ruim\".\n", "the round has no verdict"},
		{"NFD Metodo", "Barra: x\nRodada 1 — vitória\nMe\u0301todo: \"cego\"\n", "\"Rodada 1\" has no judge quote"},
		{"Saida inside a round", "Barra: x\nRodada 1: \"c\"\nsaída: empate\n", "\"Rodada 1\" has no verdict"},
		{"single list item stays clean", "Barra: x\n- **vitória** — juiz: \"x\"\n", ""},
		{"implicit round reads the whole block", "Barra: x\nvitória no eixo\n\njuiz disse \"x\"\n", ""},
	})
}

// C7: more markers and NBSP.
func TestGauntletMarkersAndSpaces(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"N) numbering", "Barra: x\nRodada 1 — vitória: \"a\"\n1) Rodada 2 — vitória\n", "\"Rodada 2\" has no judge quote"},
		{"plus bullet", "Barra: x\nRodada 1 — vitória: \"a\"\n+ Rodada 2 — vitória\n", "\"Rodada 2\" has no judge quote"},
		{"NBSP before number", "Barra: x\nRodada 1 — vitória: \"a\"\nRodada\u00a02 — vitória\n", "\"Rodada 2\" has no judge quote"},
		{"leading NBSP", "Barra: x\nRodada 1 — vitória: \"a\"\n\u00a0\u00a0Rodada 2 — vitória\n", "\"Rodada 2\" has no judge quote"},
	})
}

// C8+C12: what counts as a quoted span.
func TestGauntletQuoteShapes(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"two spaced empty pairs", "Barra: x\nRodada 1 — vitória: \"\" \"\"\n", "\"Rodada 1\" has no judge quote"},
		{"quote inside code span", "Barra: x\nRodada 1 — vitória: `go test -run \"TestX\"`\n", "\"Rodada 1\" has no judge quote"},
		{"wrapped quote pairs across lines", "Barra: x\nRodada 1 — vitória: \"linha um\nlinha dois\"\n", ""},
		{"mixed open curly close straight", "Barra: x\nRodada 1 — vitória: “hierarquia clara\"\n", ""},
		{"right curly both sides", "Barra: x\nRodada 1 — vitória: ”x”\n", ""},
		{"guillemets stay their own pair", "Barra: x\nRodada 1 — vitória: «x\"\n", "\"Rodada 1\" has no judge quote"},
	})
}

// Criterion 7: grandfathering through the strict cutoff.
func TestGauntletGrandfather(t *testing.T) {
	before := resultGauntletStrictSince.Add(-24 * time.Hour)
	after := resultGauntletStrictSince.Add(24 * time.Hour)
	noBar := "## Gauntlet\nRodada 1 \u2014 **vit\u00f3ria**: \"cita\"\n"
	noBarNFD := "## Gauntlet\nRodada 1 \u2014 vito\u0301ria: \u201ccita\u201d\n"
	noRoundBar := "## Gauntlet\n- **vit\u00f3ria** \u2014 juiz: \"cita\"\n"
	oldRecorded := "## Resultado\n\nfeito\n\n## Gauntlet\n\nBarra: middleware do finance \u2014 apps/finance/web/middleware.ts\nRodada 1 \u2014 **vit\u00f3ria**: \"S\u00f3 A tira do matcher as rotas p\u00fablicas\"\nAchados do cr\u00edtico: 2 \u2014 corrigidos 1; refutados 1 com file:line\n\n## D\u00e9bitos\n\nnada\n"
	oldExempt := "## Resultado\n\nfeito\n\n## Gauntlet\n\nBarra: dispensada por decis\u00e3o humana (\"segue sem barra dessa vez\")\n"
	oldNFD := "## Gauntlet\nBarra: x\nRodada 1 \u2014 vito\u0301ria: \u201cita\u00e7a\u0303o\u201d\n"
	cases := []struct {
		name   string
		at     time.Time
		result string
		want   string
	}{
		{"old table: recorded before cutoff", before, oldRecorded, ""},
		{"old table: exempt before cutoff", before, oldExempt, ""},
		{"old table: nfd before cutoff", before, oldNFD, ""},
		{"legacy-only record before cutoff", before, noBar, ""},
		{"legacy-only nfd before cutoff", before, noBarNFD, ""},
		{"legacy-only implicit round before cutoff", before, noRoundBar, ""},
		{"legacy-only record after cutoff", after, noBar, `no "Barra:" line`},
		{"legacy-only nfd after cutoff", after, noBarNFD, `no "Barra:" line`},
		{"legacy-only implicit round after cutoff", after, noRoundBar, `no "Barra:" line`},
		{"before cutoff, legacy fails too", before, "## Gauntlet\nBarra: x\nRodada 1 \u2014 vit\u00f3ria\n", "\"Rodada 1\" has no judge quote"},
		{"complete record after cutoff", after, "## Gauntlet\nBarra: x\nRodada 1 \u2014 vit\u00f3ria: \"cita\"\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := store.Feature{ID: "F-0001", Title: "t", State: domain.StateImplemented}
			if err := store.SaveFeature(dir, f); err != nil {
				t.Fatal(err)
			}
			writeDoc(t, dir, "content/features/F-0001/result.md", tc.result)
			events := []domain.Event{{EntityID: "F-0001", Type: "feature.completed", At: tc.at}}
			got := resultGauntletFindings(dir, []store.Feature{f}, events)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("want clean, got %v", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0].Detail, tc.want) {
				t.Fatalf("want finding with %q, got %v", tc.want, got)
			}
		})
	}
}

// TestGauntletRoundsCountsDefeats pins F-0321's criteria 4-5: a result's
// returns are its distinct rounds whose "Rodada N" line opens with derrota —
// read by the same parser as result-missing-gauntlet — and an exemption or no
// section is out of the guard, never zero returns.
func TestGauntletRoundsCountsDefeats(t *testing.T) {
	cases := []struct {
		name     string
		result   string
		has      bool
		derrotas int
	}{
		{"two defeats and a win", "## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\nRodada 2 — derrota: \"b\"\nRodada 3 — vitória: \"c\"\n", true, 2},
		{"same round on two lines", "## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\nRodada 1 — derrota: \"a de novo\"\n", true, 1},
		{"first verdict word decides", "## Gauntlet\n\nBarra: x\nRodada 1 — vitória: \"a derrota do outro\"\n", true, 0},
		{"critic findings never count", "## Gauntlet\n\nBarra: x\nRodada 1 — vitória: \"a\"\nAchados do crítico: 1\n- Rodada 2 → a derrota que o crítico citou\n", true, 0},
		{"scaffold comment never counts", "## Gauntlet\n\nBarra: x\n<!-- Rodada 1 — derrota -->\nRodada 1 — vitória: \"a\"\n", true, 0},
		{"not applicable", "## Gauntlet\n\nBarra: não aplicável (sem barra comparável)\n", false, 0},
		{"human waiver", "## Gauntlet\n\nBarra: dispensada por decisão humana (D-0001)\n", false, 0},
		{"no section", "## Resultado\n\nfeito\n", false, 0},
		// C6 — shapes copied (trimmed) from real hub records.
		{"F-0594: VEREDITO literals over six rounds", "## Gauntlet\n\n**Barra:** o card IDENTIDADE/PREFER\u00caNCIAS de `/me/profile` como est\u00e1 no `main`\n" +
			"**Rodadas:** 6. Derrotas em 1\u20135, vit\u00f3ria na 6.\n\n> **Rodada 1 \u2014 VEREDITO: B.** \"Sim, uma trai: **T2**.\"\n\n" +
			"> **Rodada 4 \u2014 VEREDITO: B.** \"dois vocabul\u00e1rios de pill\"\n\n> **Rodada 6 \u2014 VEREDITO: A.** \"N\u00e3o.\"\n\n" +
			"O que cada derrota comprou, na ordem em que os cr\u00edticos apontaram: se\u00e7\u00f5es da Vis\u00e3o geral\n", true, 2},
		{"F-0602: implicit round with Resultado: DERROTA", "## Gauntlet\n\n**Barra:** `packages/platform-shell/src/server/session-guard.ts`\n" +
			"**M\u00e9todo:** compara\u00e7\u00e3o cega lado a lado\n**Resultado: DERROTA.** Veredito citado:\n\n> VENCEDOR: artefato-1 \u2014 \"Artefato-1 p\u00f5e o gate\"\n", true, 1},
		{"F-0601: SPLIT then VITÓRIA", "## Gauntlet\n\n**Barra:** [[F-0595]] \u2014 allow-list de post-logout\n\n" +
			"**Rodada 1 \u2014 SPLIT: \"boa parte do diff \u00e9 justamente o 'melhora a mensagem'\"** (2026-08-28 \u2014 uma vit\u00f3ria e uma derrota, o que abre nova rodada)\n\n" +
			"> \"O 409 de borda \u00e9 copy-paste id\u00eantico\"\n\n**Rodada 2 \u2014 VIT\u00d3RIA (2/2 para a entrega): \"Ataca as duas causas do 409\"**\n", true, 1},
		{"F-0645: exempt bar with alternative-standard defeats", "## Gauntlet\nBarra: n\u00e3o aplic\u00e1vel (corre\u00e7\u00e3o de infraestrutura sem artefato compar\u00e1vel)\n" +
			"Rodada 1 \u2014 derrota: \"VEREDITO: N\u00c3O \u2014 findings 1\u20133\"\nAchados do cr\u00edtico (rodada 1): 12 \u2014 corrigidos 1\n" +
			"Rodada 2 \u2014 derrota: \"Verdict: N\u00c3O \u2014 #1 removes the fallback\"\nAchados do cr\u00edtico (rodada 2): 9 \u2014 corrigidos 1\n", true, 2},
		{"verdict on the line after a heading", "## Gauntlet\n\nBarra: x\n\n### Rodada 1\n\nVeredito: derrota \u2014 \"o juiz\"\n\n### Rodada 2\n\nVeredito: vit\u00f3ria \u2014 \"ok\"\n", true, 1},
		{"exempt section above a real record", "## Gauntlet\n\nBarra: n\u00e3o aplic\u00e1vel (s\u00f3 doc)\n\n## Gauntlet\n\nBarra: x\nRodada 1 \u2014 derrota: \"j\"\nRodada 2 \u2014 vit\u00f3ria: \"k\"\n", true, 1},
		{"one round, outcome unreadable", "## Gauntlet\n\nBarra: x\nRodada 1 \u2014 SPLIT: \"um cr\u00edtico para cada lado\"\n", false, 0},
		{"implicit round, outcome unreadable", "## Gauntlet\n\nBarra: x\nrodamos o loop, confia \"x\"\n", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			has, n := GauntletRounds([]byte(tc.result))
			if has != tc.has || n != tc.derrotas {
				t.Fatalf("GauntletRounds = (%v, %d), want (%v, %d)", has, n, tc.has, tc.derrotas)
			}
		})
	}
}

// F-0875 r1: GauntletVerdict reads the same record as the lint — a verdict on
// a line of its own, with no "Rodada N —" anchor, is read like GauntletRounds
// reads it, so the accept queue never contradicts result-missing-gauntlet.
func TestGauntletVerdictReadsLikeTheLint(t *testing.T) {
	for name, tc := range map[string]struct {
		result string
		want   string
		ok     bool
	}{
		"line of its own":    {"## Gauntlet\n\nBarra: x.\n\nVeredito: derrota\n", "derrota", true},
		"resultado in bold":  {"## Gauntlet\n\nBarra: x.\n\n**Resultado:** Vitória.\n", "vitória", true},
		"opens with outcome": {"## Gauntlet\n\nBarra: x.\n\nEmpate: \"os dois\"\n", "empate", true},
		"anchored wins":      {"## Gauntlet\n\nBarra: x.\n\nVeredito: derrota\n\nRodada 2 — vitória: \"sim\"\n", "vitória", true},
		"last round decides": {"## Gauntlet\n\nRodada 1 — derrota: \"a\"\nRodada 2 — empate: \"b\"\n", "empate", true},
		"exemption":          {"## Gauntlet\n\nBarra: não aplicável (sem código).\n", "dispensada", true},
		"nothing":            {"## Gauntlet\n\nBarra: x.\n", "", false},
		"same round twice":   {"## Gauntlet\n\nRodada 1 — vitória: \"a\"\nRodada 1 — empate: \"b\"\n", "vitória", true},
		"derrota outranks":   {"## Gauntlet\n\nRodada 1 — vitória: \"a\"\nRodada 1 — derrota: \"b\"\n", "derrota", true},
		"exemption + line":   {"## Gauntlet\n\nBarra: não aplicável (sem código).\n\nVeredito: derrota\n", "dispensada", true},
	} {
		got, ok := GauntletVerdict([]byte(tc.result))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", name, got, ok, tc.want, tc.ok)
		}
	}
}
