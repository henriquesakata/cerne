package gauntlet

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/lint"
)

const quotedVerdict = "A abordagem B grava sem prosa.\n\nAnálise longa. Mais uma frase.\n\n\"B grava o resultado sem prosa à mão e o gate o reconhece.\"\n\nVEREDITO: B\n"

const resultBarOnly = "# R\n\n## Resultado\n\nok\n\n## Gauntlet\n\nBarra: o carimbo da F-0309\nMétodo: comparação cega\n\n## Outra\n\nfim\n"

func TestAddRoundOutcomes(t *testing.T) {
	for _, c := range []struct {
		verdict, ours, want string
	}{
		{"VEREDITO: A", "A", "vitória"},
		{"VEREDITO: A", "B", "derrota"},
		{"VEREDITO: B", "B", "vitória"},
		{"VEREDITO: B", "A", "derrota"},
		{"VEREDITO: EMPATE", "A", "empate"},
		{"VEREDITO: EMPATE", "b", "empate"},
		{"verdict: tie", "A", "empate"},
	} {
		out, n, outcome, err := AddRound(resultBarOnly, doclang.PT, "Frase.\n"+c.verdict+"\n", c.ours, "")
		if err != nil || n != 1 || outcome != c.want {
			t.Fatalf("%v %v: n=%d outcome=%q err=%v", c.verdict, c.ours, n, outcome, err)
		}
		if !strings.Contains(out, "Rodada 1 — "+c.want+": \"Frase.\"\n") {
			t.Errorf("line missing:\n%s", out)
		}
	}
}

func TestAddRoundEnglish(t *testing.T) {
	out, _, outcome, err := AddRound("## Gauntlet\n\nBar: x\n", doclang.EN, "Fine.\nVERDICT: A\n", "B", "")
	if err != nil || outcome != "loss" || !strings.Contains(out, "Round 1 — loss: \"Fine.\"") {
		t.Fatalf("%q %v\n%s", outcome, err, out)
	}
}

func TestAddRoundRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		verdict, ours string
		want          error
	}{
		"no verdict line":  {"Análise.\n\"Citação.\"\n", "A", ErrNoVerdict},
		"verdict not last": {"VEREDITO: A\nmais texto\n", "A", ErrNoVerdict},
		"prose verdict":    {"Frase.\nVEREDITO: A é melhor\n", "A", ErrNoVerdict},
		"empty file":       {"\n\n", "A", ErrNoVerdict},
		"bad ours":         {"Frase.\nVEREDITO: A\n", "C", ErrBadOurs},
		"no quote at all":  {"VEREDITO: A\n", "A", ErrNoQuote},
	} {
		out, _, _, err := AddRound(resultBarOnly, doclang.PT, c.verdict, c.ours, "")
		if !errors.Is(err, c.want) || out != "" {
			t.Errorf("%s: err=%v out=%q", name, err, out)
		}
	}
}

func TestQuoteChoice(t *testing.T) {
	for name, c := range map[string]struct{ verdict, flag, want string }{
		"flag wins":          {quotedVerdict, "citação própria", "citação própria"},
		"last quoted line":   {quotedVerdict, "", "B grava o resultado sem prosa à mão e o gate o reconhece."},
		"curly quotes":       {"Primeira.\n“Dito pelo juiz.”\nVEREDITO: A\n", "", "Dito pelo juiz."},
		"last of two quoted": {"\"Um.\"\n\"Dois.\"\nVEREDITO: A\n", "", "Dois."},
		"first sentence":     {"A resposta B é melhor. Porque sim.\nVEREDITO: A\n", "", "A resposta B é melhor."},
		"inner quotes":       {"Disse \"x\" e “y”. Fim.\nVEREDITO: A\n", "", "Disse 'x' e 'y'."},
		"multiline sentence": {"Uma frase\nem duas linhas. Outra.\nVEREDITO: A\n", "", "Uma frase em duas linhas."},
		"flag inner quotes":  {"Z.\nVEREDITO: A\n", "diz \"a\"", "diz 'a'"},
	} {
		out, _, _, err := AddRound(resultBarOnly, doclang.PT, c.verdict, "A", c.flag)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(out, ": \""+c.want+"\"\n") {
			t.Errorf("%s: want quote %q in\n%s", name, c.want, out)
		}
	}
}

func TestNumbering(t *testing.T) {
	r := "## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\n"
	out, n, _, err := AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if err != nil || n != 2 {
		t.Fatalf("n=%d %v", n, err)
	}
	out, n, _, _ = AddRound(out, doclang.PT, "G.\nVEREDITO: A\n", "A", "")
	if n != 3 || !strings.Contains(out, "Rodada 3 — vitória") {
		t.Fatalf("n=%d\n%s", n, out)
	}
	// a Round written in the other language still counts
	_, n, _, _ = AddRound("## Gauntlet\n\nRound 4 — win: \"a\"\n", doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if n != 5 {
		t.Errorf("n=%d", n)
	}
}

func TestInsertionPoint(t *testing.T) {
	// after the bar and its header lines, inside the section
	out, _, _, _ := AddRound(resultBarOnly, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.Contains(out, "Método: comparação cega\nRodada 1 — vitória: \"F.\"\n\n## Outra") {
		t.Errorf("after bar:\n%s", out)
	}
	// after the last round / critic line
	r := "## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\nAchados do crítico: um\n\n## Z\n"
	out, _, _, _ = AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.Contains(out, "Achados do crítico: um\nRodada 2 — vitória: \"F.\"\n\n## Z") {
		t.Errorf("after last:\n%s", out)
	}
	// section absent: created at the end
	out, _, _, _ = AddRound("# R\n\n## Resultado\n\nok\n", doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.HasSuffix(out, "ok\n\n## Gauntlet\n\nRodada 1 — vitória: \"F.\"\n") {
		t.Errorf("created:\n%s", out)
	}
	out, _, _, _ = AddRound("# R\n", doclang.EN, "F.\nVERDICT: A\n", "A", "")
	if !strings.Contains(out, "\n## Gauntlet\n") {
		t.Errorf("EN heading:\n%s", out)
	}
	// a fenced "## Gauntlet" example is not the section
	r = "## Gauntlet\n\nBarra: x\n\n```\n## Gauntlet\n```\n"
	out, _, _, _ = AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.Contains(out, "Barra: x\nRodada 1") {
		t.Errorf("fence:\n%s", out)
	}
}

func TestAddCritic(t *testing.T) {
	out, err := AddCritic(resultBarOnly, doclang.PT, "2 — corrigidos 1; refutados 1")
	if err != nil || !strings.Contains(out, "Método: comparação cega\nAchados do crítico: 2 — corrigidos 1; refutados 1\n") {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _ = AddCritic(out, doclang.PT, "(b) 1 aceito")
	if strings.Count(out, "Achados do crítico:") != 2 || !strings.Contains(out, "refutados 1\nAchados do crítico: (b) 1 aceito\n") {
		t.Errorf("appended:\n%s", out)
	}
	out, _ = AddCritic("## Gauntlet\n\nBar: x\n", doclang.EN, "none")
	if !strings.Contains(out, "Critic findings: none\n") {
		t.Errorf("EN:\n%s", out)
	}
	if _, err := AddCritic(resultBarOnly, doclang.PT, "  "); !errors.Is(err, ErrEmptyCritic) {
		t.Errorf("empty: %v", err)
	}
}

// The recorded lines are the lint's own: GauntletRounds reads them.
func TestRecordedLinesAreReadByTheLint(t *testing.T) {
	r := resultBarOnly
	r, _, _, _ = AddRound(r, doclang.PT, quotedVerdict, "A", "") // B wins: derrota
	r, _ = AddCritic(r, doclang.PT, "1 — corrigido")
	has, derrotas := lint.GauntletRounds([]byte(r))
	if !has || derrotas != 1 {
		t.Fatalf("after loss: has=%v derrotas=%d\n%s", has, derrotas, r)
	}
	r, _, _, _ = AddRound(r, doclang.PT, quotedVerdict, "B", "")
	has, derrotas = lint.GauntletRounds([]byte(r))
	if !has || derrotas != 1 {
		t.Fatalf("after win: has=%v derrotas=%d\n%s", has, derrotas, r)
	}
}

func TestHTMLCommentsAreNeverRecord(t *testing.T) {
	r := "## Resultado\n\nok\n\n<!--\n## Gauntlet\nBarra: exemplo\n-->\n\n## Gauntlet\n\n<!--\nBarra: <ref>\nRodada 7 — vitória: \"exemplo\"\nAchados do crítico: exemplo\n-->\nBarra: real\n\n## Z\n"
	out, n, _, err := AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v (a commented Rodada 7 was counted)", n, err)
	}
	if !strings.Contains(out, "Barra: real\nRodada 1 — vitória: \"F.\"\n\n## Z") {
		t.Errorf("wrote in the wrong place:\n%s", out)
	}
	// only a commented heading: the section does not exist yet
	r = "## Resultado\n\nok\n\n<!--\n## Gauntlet\nBarra: exemplo\n-->\n"
	out, _, _, _ = AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.HasSuffix(out, "-->\n\n## Gauntlet\n\nRodada 1 — vitória: \"F.\"\n") {
		t.Errorf("commented heading taken as the section:\n%s", out)
	}
	// a comment trailing the last record line
	r = "## Gauntlet\n\nBarra: x\n<!-- nota\nRodada 3 — derrota: \"a\"\n-->\n"
	_, n, _, _ = AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if n != 1 {
		t.Errorf("n=%d", n)
	}
}

func TestContinuationLinesStayWithTheirRecord(t *testing.T) {
	r := "## Gauntlet\n\nBarra: x\nAchados do crítico: 2\n- (a) um\n  continua\n- (b) dois\n\n## Z\n"
	out, _, _, _ := AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.Contains(out, "- (b) dois\nRodada 1 — vitória: \"F.\"\n\n## Z") {
		t.Errorf("after bullets:\n%s", out)
	}
	r = "## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\n(juiz cego, contexto novo)\n\n## Z\n"
	out, _, _, _ = AddRound(r, doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if !strings.Contains(out, "(juiz cego, contexto novo)\nRodada 2 — vitória: \"F.\"\n\n## Z") {
		t.Errorf("after parenthetical:\n%s", out)
	}
	has, derrotas := lint.GauntletRounds([]byte(out))
	if !has || derrotas != 1 {
		t.Errorf("attribution: has=%v derrotas=%d\n%s", has, derrotas, out)
	}
	// a record line at the very end still gets its new line
	out, _ = AddCritic("## Gauntlet\n\nRodada 1 — derrota: \"a\"\n(nota)", doclang.PT, "x")
	if !strings.HasSuffix(out, "(nota)\nAchados do crítico: x") {
		t.Errorf("eof:\n%q", out)
	}
}

func TestEmptyQuoteMatchIsSkipped(t *testing.T) {
	// a quoted line that is only blank inside is not a citation: keep looking
	out, _, _, err := AddRound(resultBarOnly, doclang.PT, "Primeira frase.\n\"Boa.\"\n\" \"\nVEREDITO: A\n", "A", "")
	if err != nil || !strings.Contains(out, ": \"Boa.\"\n") {
		t.Errorf("%v\n%s", err, out)
	}
	out, _, _, err = AddRound(resultBarOnly, doclang.PT, "Primeira frase.\n\" \"\nVEREDITO: A\n", "A", "")
	if err != nil || !strings.Contains(out, ": \"Primeira frase.\"\n") {
		t.Errorf("must fall back to the first sentence: %v\n%s", err, out)
	}
	out, _, _, _ = AddRound(resultBarOnly, doclang.PT, "X.\n\"a\" e \"b\"\nVEREDITO: A\n", "A", "")
	if !strings.Contains(out, ": \"a' e 'b\"\n") {
		t.Errorf("two quotes in one line:\n%s", out)
	}
}

func TestCRLFKept(t *testing.T) {
	r := strings.ReplaceAll(resultBarOnly, "\n", "\r\n")
	out, _, _, _ := AddRound(r, doclang.PT, "F.\r\nVEREDITO: A\r\n", "A", "")
	if strings.Count(out, "\r\n") != strings.Count(out, "\n") || !strings.Contains(out, "Rodada 1 — vitória: \"F.\"\r\n") {
		t.Errorf("mixed endings: %q", out)
	}
	out, _, _, _ = AddRound("# R\r\n", doclang.PT, "F.\nVEREDITO: A\n", "A", "")
	if strings.Count(out, "\r\n") != strings.Count(out, "\n") {
		t.Errorf("created section: %q", out)
	}
}

func TestVerdictLine(t *testing.T) {
	for _, c := range []struct {
		name, text, want string
		ok               bool
	}{
		{"pt", "Razão.\n\nVEREDITO: B\n", "B", true},
		{"en tie", "Reason.\nVERDICT: TIE", "TIE", true},
		{"emphasis and trailing blanks", "x\n**VEREDITO: A**\n\n  \n", "A", true},
		{"verdict not last", "VEREDITO: A\nmas depois, mais texto", "", false},
		{"none", "só texto", "", false},
		{"empty", "", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := VerdictLine(c.text)
			if got != c.want || ok != c.ok {
				t.Fatalf("VerdictLine = %q, %v; want %q, %v", got, ok, c.want, c.ok)
			}
		})
	}
}
