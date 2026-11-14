package app

import (
	"strings"
	"testing"
)

// TestAcceptQueue_EnglishProduction (F-0418 crit. 8): the accept queue reads
// production evidence written in English like the Portuguese, in any
// project, and the English negatives as absence — without reading a
// Portuguese line through an English word ("no ar" is evidence).
func TestAcceptQueue_EnglishProduction(t *testing.T) {
	for _, tc := range []struct{ result, want string }{
		{"## Result\n\nIn production: v1.2 deployed on 2026-11-14\n", "v1.2 deployed on 2026-11-14"},
		{"- **Production:** served at /health\n", "served at /health"},
		{"## Production\n\nv1.2 deployed, logs clean\n", "v1.2 deployed, logs clean"},
		{"## In production\n\nrunning since Monday\n", "running since Monday"},
		{"In production: not yet\n", ""},
		{"Production: pending\n", ""},
		{"Production: none\n", ""},
		{"Production: no\n", ""},
		{"Production: No, the deploy was rolled back\n", ""},
		{"Production: no information yet\n", ""},
		{"Production: n/a\n", ""},
		{"Production: TBD\n", ""},
		{"Production: no evidence yet\n", ""},
		{"Production: awaiting the deploy\n", ""},
		{"Production: to verify\n", ""},
		{"## Production\n\nnothing yet\n", ""},
		// pt-BR, as before F-0418.
		{"Em produção: v1.2 no ar\n", "v1.2 no ar"},
		{"Em produção: no database primário, 200 OK\n", "no database primário"},
		{"Em produção: no dataset de prod, sem erro\n", "no dataset de prod"},
		{"Em produção: no informe semanal\n", "no informe semanal"},
		{"## Produção\n\nno ar desde segunda\n", "no ar desde segunda"},
		{"Em produção: ainda não\n", ""},
		{"Produção: pendente\n", ""},
	} {
		got := summarizeResult([]byte(tc.result)).Production
		if tc.want == "" && got != NotInformed || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%q: production = %q, want %q", tc.result, got, tc.want)
		}
	}
}
