package doccheck

import (
	"errors"
	"testing"
)

// B-0078: the spec readers (bar, level, judge question) take a "<!--" inside a
// code span as text, so a "-->" quoted further down closes nothing.
const (
	openInCode  = "Prosa com `a <!-- b` no meio.\n"
	closeInCode = "Mais: `c --> d`.\n"
	specBar     = "Barra: a referência X\nMétodo: m\nSaída: s\n"
)

// B-0078 crit. 1
func TestSpecBar_CommentOpenerInCode(t *testing.T) {
	body := "## Problema\n" + openInCode + "## Verificação\n" + specBar + closeInCode
	if _, err := SpecBar([]byte(body)); err != nil {
		t.Fatalf("SpecBar = %v, want the Barra line read", err)
	}
}

// B-0078 crit. 2
func TestSpecLevel_CommentOpenerInCode(t *testing.T) {
	body := openInCode + "**Nível:** leve\n" + closeInCode
	if got, ok := SpecLevel([]byte(body)); got != "leve" || !ok {
		t.Fatalf("SpecLevel = %q, %v; want \"leve\", true", got, ok)
	}
}

// B-0078 crit. 3
func TestSpecJudgeQuestion_CommentOpenerInCode(t *testing.T) {
	body := "## Verificação\n" + openInCode + specBar + "Pergunta ao juiz: qual é melhor?\n" + closeInCode
	if got, err := SpecJudgeQuestion([]byte(body)); got != "qual é melhor?" || err != nil {
		t.Fatalf("SpecJudgeQuestion = %q, %v; want the question", got, err)
	}
}

// B-0078 crit. 4: the line of a misplaced question still counts the original
// body, past a multi-line comment and a code-span opener.
func TestSpecJudgeQuestion_MisplacedLinePastCodeOpener(t *testing.T) {
	body := "<!-- guia\nem duas linhas -->\n" + openInCode + "## Desenho\nPergunta ao juiz: qual?\n" + closeInCode + "## Verificação\n" + specBar
	_, err := SpecJudgeQuestion([]byte(body))
	var m *MisplacedJudgeQuestion
	if !errors.As(err, &m) || m.Line != 5 {
		t.Fatalf("SpecJudgeQuestion err = %v, want misplaced at line 5", err)
	}
}
