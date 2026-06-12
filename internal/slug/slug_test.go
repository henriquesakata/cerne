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
		{"strips runes outside a-z0-9 hyphen", "Decisão: usar Postgres!", "deciso-usar-postgres"},
		{"empty input", "", ""},
		{"only stripped runes", "ãç!?", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Make(tt.in); got != tt.want {
				t.Fatalf("Make(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
