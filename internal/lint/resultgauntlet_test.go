package lint

import (
	"os"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
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
		{"quote on another line does not vouch", domain.StateImplemented, after, "## Gauntlet\nBarra: \"refer\u00eancia\"\nRodada 1 \u2014 vit\u00f3ria\n", "rodada 1 sem citação do juiz"},
		{"no section flags", domain.StateImplemented, after, "## Resultado\n\nfeito\n", "sem seção"},
		{"section without cited verdict flags", domain.StateImplemented, after, emptySection, "sem linha \"Barra:\""},
		{"verdict without quote flags", domain.StateImplemented, after, "## Gauntlet\nBarra: x\nRodada 1 — vitória\n", "rodada 1 sem citação do juiz"},
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
	if len(got) != 1 || !strings.Contains(got[0].Detail, "sem seção") {
		t.Fatalf("want one 'sem seção' finding, got %v", got)
	}
}

// gauntletProblem runs the round reader on a section body.
func gauntletProblem(section string) string { return parseGauntlet(section).problem() }

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	section, has := gauntletSection(b)
	if !has {
		t.Fatalf("%s has no Gauntlet section", name)
	}
	return section
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
		{"rodada mention mid-sentence opens no block", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"cita\"\nver a rodada 2 depois\n", ""},
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
		{"quote only on the Barra line", "Barra: \"ref\"\nRodada 1 \u2014 vit\u00f3ria\n", "rodada 1 sem citação do juiz"},
		{"quote only in round 1", "Barra: x\nRodada 1 \u2014 derrota: \"a\"\nRodada 2 \u2014 vit\u00f3ria\n", "rodada 2 sem citação do juiz"},
		{"quote only in Achados (NFC)", "Barra: x\nRodada 1 \u2014 vit\u00f3ria\nAchados do cr\u00edtico: 1 \u2014 \"x\"\n", "rodada 1 sem citação do juiz"},
		{"quote only in Achados (NFD)", "Barra: x\nRodada 1 \u2014 vit\u00f3ria\nAchados do cri\u0301tico: 1 \u2014 \"x\"\n", "rodada 1 sem citação do juiz"},
		{"empty quote pair", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"  \"\n", "rodada 1 sem citação do juiz"},
		{"lone quote char", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: 5\" tela\n", "rodada 1 sem citação do juiz"},
		{"table: citation only in the next row", "Barra: x\n| Rodada 1 | vit\u00f3ria |\n| Rodada 2 | vit\u00f3ria | \"y\" |\n", "rodada 1 sem citação do juiz"},
		{"only Barra, no round", "Barra: x\n", "rodada sem citação do juiz"},
		{"implicit round: quote only on Barra", "Barra: \"x\"\nvit\u00f3ria sem citar\n", "rodada sem citação do juiz"},
	})
}

// Criterion 4.
func TestGauntletRoundWithoutVerdict(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"quote, no verdict", "Barra: x\nRodada 1: \"cita\"\n", "rodada 1 sem veredito"},
		{"verdict only in other round", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\nRodada 2: \"b\"\n", "rodada 2 sem veredito"},
		{"literal EMPATE + quote", "Barra: x\nRodada 1: \"cita\"\nVEREDITO: EMPATE\n", ""},
		{"literal A, lowercase prefix", "Barra: x\nRodada 1: \"cita\"\nveredito: a\n", ""},
		{"VEREDITO: C is no verdict", "Barra: x\nRodada 1: \"cita\"\nVEREDITO: C\n", "rodada 1 sem veredito"},
	})
}

// Criterion 5.
func TestGauntletMissingBar(t *testing.T) {
	complete := "Rodada 1 \u2014 vit\u00f3ria: \"cita\"\n"
	runGauntletCases(t, []gauntletCase{
		{"no Barra line", complete, `sem linha "Barra:"`},
		{"empty Barra", "Barra:\n" + complete, `sem linha "Barra:"`},
		{"bold Barra without value", "**Barra:**\n" + complete, `sem linha "Barra:"`},
		{"bold list Barra with value", "- **Barra:** x\n" + complete, ""},
		{"quoted Barra", "> Barra: x\n" + complete, ""},
		{"Barra mid-sentence is no Barra line", "ver Barra: x\n" + complete, `sem linha "Barra:"`},
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
		{"exemption without parenthesis is no exemption", "Barra: n\u00e3o aplic\u00e1vel\n", "rodada sem citação do juiz"},
	})
}

// Criterion 8.
func TestGauntletProblemOrder(t *testing.T) {
	runGauntletCases(t, []gauntletCase{
		{"no bar and incomplete round: bar only", "Rodada 1 \u2014 vit\u00f3ria\n", `sem linha "Barra:"`},
		{"quote before verdict", "Barra: x\nRodada 1\n", "rodada 1 sem citação do juiz"},
		{"first incomplete round named", "Barra: x\nRodada 1: \"a\" VEREDITO: A\nRodada 2: \"b\"\nRodada 3\n", "rodada 2 sem veredito"},
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
		{"incomplete numbered round still flags", "Barra: x\nRodada 1 \u2014 vit\u00f3ria: \"a\"\nRodada 2 \u2014 correções\n### Rodada 2 \u2014 notas\n", "rodada 2 sem citação do juiz"},
		{"merged round, quote but no verdict", "Barra: x\nRodada 2: \"q\"\nRodada 2 \u2014 notas\n", "rodada 2 sem veredito"},
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
		{"mid-line after round anchor", "**Rodada 1 \u2014 vit\u00f3ria.** Barra: x \"c\"\n", `sem linha "Barra:"`},
		{"prose exemption mid-line", "A spec fixou Barra: n\u00e3o aplic\u00e1vel (y)\n" + round, `sem linha "Barra:"`},
		{"veredito barra prose", "Veredito: barra n\u00e3o aplic\u00e1vel (y)\n" + round, `sem linha "Barra:"`},
		{"qualifier over 40 chars", long + round, `sem linha "Barra:"`},
		{"no colon", "Barra da spec x\n" + round, `sem linha "Barra:"`},
		{"empty value", "Barra da spec:\n" + round, `sem linha "Barra:"`},
		{"empty bold value", "**Barra (da spec):**\n" + round, `sem linha "Barra:"`},
		{"word glued to Barra is no qualifier", "Barras: x\n" + round, `sem linha "Barra:"`},
	})
}

// Criterion 7: grandfathering through the strict cutoff.
func TestGauntletGrandfather(t *testing.T) {
	before := resultGauntletStrictSince.Add(-24 * time.Hour)
	after := resultGauntletStrictSince.Add(24 * time.Hour)
	noBar := "## Gauntlet\nRodada 1 \u2014 **vit\u00f3ria**: \"cita\"\n"
	noBarNFD := "## Gauntlet\nRodada 1 \u2014 vito\u0301ria: \u201ccita\u201d\n"
	noRoundBar := "## Gauntlet\n- **vit\u00f3ria** \u2014 juiz: \"cita\"\n"
	cases := []struct {
		name   string
		at     time.Time
		result string
		want   string
	}{
		{"legacy-only record before cutoff", before, noBar, ""},
		{"legacy-only nfd before cutoff", before, noBarNFD, ""},
		{"legacy-only implicit round before cutoff", before, noRoundBar, ""},
		{"legacy-only record after cutoff", after, noBar, `sem linha "Barra:"`},
		{"legacy-only nfd after cutoff", after, noBarNFD, `sem linha "Barra:"`},
		{"legacy-only implicit round after cutoff", after, noRoundBar, `sem linha "Barra:"`},
		{"before cutoff, legacy fails too", before, "## Gauntlet\nBarra: x\nRodada 1 \u2014 vit\u00f3ria\n", "rodada 1 sem citação do juiz"},
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
