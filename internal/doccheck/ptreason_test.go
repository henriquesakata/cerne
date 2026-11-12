package doccheck

import "testing"

// TestPTReason pins the detector the F-0124 guard and the F-0353 lint share.
func TestPTReason(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"nenhuma feature encontrada", `pt-BR word "nenhuma"`},
		{"cerne: clone de cache ausente", `pt-BR word "de"`},
		{"prioridade inválida", `diacritic 'á'`},
		{"no such feature: do it via gitlab.com", ""},
		{"line 3: no feature found", ""},
	} {
		if got := PTReason(c.in); got != c.want {
			t.Errorf("PTReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
