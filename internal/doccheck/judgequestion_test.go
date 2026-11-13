package doccheck

import (
	"errors"
	"testing"
)

func TestSpecJudgeQuestion(t *testing.T) {
	const bar = "Barra: a referência X\nMétodo: m\nSaída: s\n"
	for name, tt := range map[string]struct {
		body string
		want string
		err  error
	}{
		"present":     {"## Verificação\n" + bar + "Pergunta ao juiz: qual é melhor?\n", "qual é melhor?", nil},
		"english":     {"## Verification\nBar: the reference X\nQuestion for the judge: which is better?\n", "which is better?", nil},
		"absent":      {"## Verificação\n" + bar, "", ErrJudgeQuestionMissing},
		"empty":       {"## Verificação\n" + bar + "Pergunta ao juiz:\n", "", ErrJudgeQuestionMissing},
		"placeholder": {"## Verificação\n" + bar + "Pergunta ao juiz: <a pergunta>\n", "", ErrJudgeQuestionMissing},
		"outside":     {"## Desenho\nPergunta ao juiz: qual?\n## Verificação\n" + bar, "", ErrJudgeQuestionMisplaced},
		"in comment":  {"## Verificação\n" + bar + "<!-- Pergunta ao juiz: qual? -->\n", "", ErrJudgeQuestionMissing},
		"not applic.": {"## Verificação\nBarra: não aplicável (puro texto) — padrão alternativo: teste\n", "", nil},
		"waived":      {"## Verificação\nBarra: dispensada por decisão humana (\"dispensa\")\n", "", nil},
		"no bar":      {"## Verificação\nsó texto\n", "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := SpecJudgeQuestion([]byte(tt.body))
			if got != tt.want || !errors.Is(err, tt.err) {
				t.Errorf("SpecJudgeQuestion = %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

// F-0381 round 2: a question outside Verificação is reported with its line,
// counted in the original body even past a multi-line guide comment.
func TestSpecJudgeQuestion_MisplacedLine(t *testing.T) {
	body := "<!-- guia\nem duas linhas -->\n## Desenho\nPergunta ao juiz: qual?\n## Verificação\nBarra: ref — acesso\n"
	_, err := SpecJudgeQuestion([]byte(body))
	var m *MisplacedJudgeQuestion
	if !errors.As(err, &m) || m.Line != 4 {
		t.Fatalf("err = %v, want MisplacedJudgeQuestion at line 4", err)
	}
}

func TestBarExempt(t *testing.T) {
	for body, want := range map[string]bool{
		"Barra: não aplicável (puro texto) — padrão alternativo: teste\n": true,
		"Barra: dispensada por decisão humana (\"dispensa\")\n":           true,
		"Barra: ref — acesso\n":                                           false,
		"sem barra\n":                                                     false,
	} {
		if got := BarExempt([]byte(body)); got != want {
			t.Errorf("BarExempt(%q) = %v, want %v", body, got, want)
		}
	}
}
