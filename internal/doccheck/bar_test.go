package doccheck

import (
	"errors"
	"testing"
)

// TestSpecBar pins the bar predicate (moved out of internal/app by F-0308):
// the cases of app's TestCheckSpecBar plus the accepted line. Stub detection
// stays with the callers (the document gate).
func TestSpecBar(t *testing.T) {
	cases := []struct {
		name     string
		spec     string
		wantErr  error  // nil = valid bar
		wantLine string // the accepted line, when valid
	}{
		{"named reference passes", "# t\n\n## Verificação\n\nBarra: work_gate do cerne — .cernerc [gates]\nMétodo: comparação cega\n", nil, "Barra: work_gate do cerne — .cernerc [gates]"},
		{"nao aplicavel with justification passes", "# t\n\nBarra: não aplicável (fix pontual sem artefato comparável, confirmado pelo humano) — padrão alternativo: teste de integração\n", nil, "Barra: não aplicável (fix pontual sem artefato comparável, confirmado pelo humano) — padrão alternativo: teste de integração"},
		{"dispensada with quote passes", "# t\n\nBarra: dispensada por decisão humana (\"pode seguir sem barra dessa vez\")\n", nil, "Barra: dispensada por decisão humana (\"pode seguir sem barra dessa vez\")"},
		{"no bar line refuses", "# t\n\n## Verificação\n\nSó testes unitários.\n", ErrBarMissing, ""},
		{"empty bar refuses", "# t\n\nBarra:\n", ErrBarMalformed, ""},
		{"nao aplicavel without parens refuses", "# t\n\nBarra: não aplicável\n", ErrBarMalformed, ""},
		{"dispensada without quote refuses", "# t\n\nBarra: dispensada por decisão humana\n", ErrBarMalformed, ""},
		{"bar only inside guide comment refuses", "# t\n\n## Problema\n\nProblema real descrito aqui.\n\n## Verificação\n<!-- uma das três formas: \"Barra: não\naplicável (<justificativa>)\" ou \"Barra: dispensada por decisão humana\". -->\n", ErrBarMissing, ""},
		{"bar inside code fence passes", "# t\n\n## Verificação\n\n```\nBarra: lint:i18n-baseline do CI (F-0484)\nMétodo: comparação cega\n```\n", nil, "Barra: lint:i18n-baseline do CI (F-0484)"},
		{"unaccented nao aplicavel without parens refuses", "# t\n\nBarra: nao aplicavel\n", ErrBarMalformed, ""},
		{"malformed first bar does not veto a valid later one", "# t\n\nBarra:\n\n## Verificação\n\nBarra: referência nomeada — acesso real\n", nil, "Barra: referência nomeada — acesso real"},
		// An empty "Barra:" is empty even when another line follows (F-0327).
		{"empty bar followed by another line refuses", "# t\n\n## Verificação\n\nBarra:\nMétodo: comparação cega\nSaída: vencer ou empatar\n", ErrBarMalformed, ""},
		{"empty bar with trailing blanks before another line refuses", "# t\n\nBarra: \t\nMétodo: x\n", ErrBarMalformed, ""},
		{"crlf empty bar before another line refuses", "# t\r\n\r\nBarra:\r\nMétodo: x\r\n", ErrBarMalformed, ""},
		{"empty bar does not swallow the next bar line", "# t\n\nBarra:\nBarra: não aplicável\n", ErrBarMalformed, ""},
		{"nfd accents pass", "# t\n\nBarra: na\u0303o aplica\u0301vel (sem artefato compara\u0301vel, confirmado) — padra\u0303o alternativo: invariante\n", nil, "Barra: na\u0303o aplica\u0301vel (sem artefato compara\u0301vel, confirmado) — padra\u0303o alternativo: invariante"},
		{"crlf line endings trim", "# t\r\n\r\nBarra: referência — acesso\r\n", nil, "Barra: referência — acesso"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, err := SpecBar([]byte(tc.spec))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if line != "" {
					t.Fatalf("line = %q on refusal, want empty", line)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if line != tc.wantLine {
				t.Fatalf("line = %q, want %q", line, tc.wantLine)
			}
		})
	}
}
