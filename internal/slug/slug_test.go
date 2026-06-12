package slug

import "testing"

func TestMake(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "Cerne", "cerne"},
		{"spaces to hyphens", "My Project", "my-project"},
		{"keeps digits and hyphens", "v2 multi-tenant", "v2-multi-tenant"},
		{"transliterates diacritics", "Decisão: usar Postgres!", "decisao-usar-postgres"},
		{"pt-br title keeps letters", "Consolidação de defaults obrigatórios", "consolidacao-de-defaults-obrigatorios"},
		{"uppercase accents transliterate via lowercase", "ÇÃO Édipo", "cao-edipo"},
		{"empty input", "", ""},
		{"only stripped runes", "!?§", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Make(tt.in); got != tt.want {
				t.Fatalf("Make(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
