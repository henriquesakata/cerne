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
		"outside":     {"## Desenho\nPergunta ao juiz: qual?\n## Verificação\n" + bar, "", ErrJudgeQuestionMissing},
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
